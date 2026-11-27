// Package httpapi — HTTP-хендлеры grade-api (ARCHITECTURE.md §4.1).
// WP-2: /healthz, /api/v1/auth/register, /api/v1/auth/login, /api/v1/auth/me.
package httpapi

import (
	"context"
	"database/sql"
	"log/slog"
	"sync"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/tracks"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voiceorchestrator"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Server — HTTP-сервер: хендлеры + зависимости.
type Server struct {
	cfg         *config.Config
	users       *db.UserStore
	sessions    *db.SessionStore
	submissions *db.SubmissionStore
	whiteboards *db.WhiteboardStore
	reports     *db.ReportStore
	// wsTickets — одноразовые тикеты WS-upgrade (spec-14/#12: JWT в query
	// параметре утекает в логи прокси; тикет выдаётся по REST и consumed).
	wsTickets *wsTicketStore
	// Ограничители частоты (см. ratelimit.go): логин по аккаунту, общий IP-
	// фильтр на /auth/*, регистрация по адресу, выдача ws-тикетов по
	// пользователю и запуски кода на сессию.
	loginLimit    *rateLimiter
	authIPLimit   *rateLimiter
	registerLimit *rateLimiter
	wsTicketLimit *rateLimiter
	runLimit      *rateLimiter
	// tracks — треки собеседований (spec-18): файл + admin-CRUD.
	tracks       *tracks.Store
	templates    *db.TemplateStore // итерация A: настраиваемые планы
	profiles     *db.ProfileStore  // итерация B: профили интервьюера
	ttsVoices    *ttsVoiceRegistry // F1 + admin (итерация 11): реестр голосов + reload
	openingsPath string            // итерация 11: путь к openings.json (admin-запись)
	engine       *session.Engine
	interviewer  *interviewer.Interviewer
	voice        *voicesvc.Client
	log          *slog.Logger
	// orch — DI-оркестратор голосового хода (A1): LLM→TTS (pacing, ordered-
	// синтез), barge-in. Стабильные зависимости в New; voice — параметр методов.
	orch *voiceorchestrator.Orchestrator

	// wsSessions — живые WS-соединения по ID сессии (1 сессия — 1 соединение):
	// pause во время активного хода останавливает TTS-стрим (FR-S7).
	wsMu       sync.Mutex
	wsSessions map[int64]*wsSession
}

// New собирает сервер (LLM-провайдер по конфигу: реальный клиент или мок, LLM_MOCK).
func New(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger) *Server {
	var provider llm.Provider
	if cfg.LLMMock {
		// Мок даёт осмысленные реплики, но это не модель: без строки в логе
		// «собеседование не то, что кажется» отличимо только по качеству ответов.
		log.Warn("LLM_MOCK=1: собеседование ведёт мок, ответы заготовлены")
		provider = newMockProvider(cfg)
	} else {
		provider = llm.NewClient(cfg.LLMBaseURL, cfg.LLMModel, cfg.LLMAPIKey).
			WithEnableThinking(cfg.LLMEnableThinking)
	}
	return NewWithLLM(cfg, database, dialect, log, provider)
}

// NewWithLLM — сборка с явным LLM-провайдером (тесты/ди, ADR-005).
func NewWithLLM(cfg *config.Config, database *sql.DB, dialect db.Dialect, log *slog.Logger,
	provider llm.Provider) *Server {
	users := db.NewUserStore(database, dialect)
	sessions := db.NewSessionStore(database, dialect)
	submissions := db.NewSubmissionStore(database, dialect)
	whiteboards := db.NewWhiteboardStore(database, dialect)
	reports := db.NewReportStore(database, dialect)
	templates := db.NewTemplateStore(database, dialect)
	if err := templates.EnsureDefaults(context.Background(), interviewer.DefaultPrograms()); err != nil {
		log.Warn("сид дефолтных шаблонов не удался (используется gradeProgram)", "err", err)
	}
	wsTickets := newWSTicketStore()
	// Частотные лимиты. Значения из практики живого стенда, не «с потолка»:
	// 8 попыток входа на аккаунт за 5 мин хватает на честного пользователя,
	// который трижды опечатался, и не оставляет места для перебора.
	loginLimit := newRateLimiter(8, 5*time.Minute)    // по аккаунту
	authIPLimit := newRateLimiter(60, 10*time.Minute) // по адресу (best effort)
	registerLimit := newRateLimiter(10, time.Hour)    // по адресу (best effort)
	wsTicketLimit := newRateLimiter(60, time.Minute)  // по пользователю
	runLimit := newRateLimiter(6, time.Minute)        // запусков кода на сессию
	// spec-18: треки собеседований (config/roles.json; оверрайд
	// ROLE_TRACKS_FILE) — до построения engine/interviewer (DI резолверов).
	// Ошибка файла — не валит старт: in-memory дефолты (сигнатуры New не
	// возвращают error; файл восстанавливается при первой admin-записи).
	tracksStore, trackErr := tracks.Load(resolveConfigPath("ROLE_TRACKS_FILE", "config/roles.json"), log)
	if trackErr != nil {
		log.Error("tracks: файл не загружен — in-memory дефолты", "err", trackErr)
		tracksStore, trackErr = tracks.Load("", log)
		if trackErr != nil {
			log.Error("tracks: дефолты не провалидировались", "err", trackErr)
			tracksStore = tracks.NewMemory()
		}
	}
	profiles := db.NewProfileStore(database, dialect)
	// Пресеты профилей интервьюера (идемпотентно, итерация B).
	if err := profiles.EnsurePresets(context.Background(), db.PresetProfiles()); err != nil {
		log.Warn("profiles: пресеты не сидированы", "err", err)
	}
	engineOpts := []session.Opt{
		session.WithPauseTimeout(time.Duration(cfg.PauseTimeoutS) * time.Second),
		session.WithSessionLimit(cfg.SessionLimitS),
		session.WithTemplates(templates),
		// spec-18: резолвер треков (программа стадий/доска/критерии).
		session.WithTracks(func(trackID string) (*models.Track, error) {
			return tracksStore.Get(trackID)
		}),
	}
	// Отладка стадий (dev, ENABLE_DEBUG=1): прыжок на любую стадию программы роли,
	// чтобы не прогонять интервью с начала ради одной доски/практики.
	if cfg.DebugEndpoints {
		engineOpts = append(engineOpts, session.WithDebugStages())
	}
	engine := session.New(sessions, users, log, engineOpts...)
	// F2 (Итерация 10) + Admin (итерация 11): настраиваемые случайные начала
	// (по грейду). Путь — env INTRO_OPENINGS_FILE, дефолт config/openings.json
	// (CWD-независимо, BUG-012). Итерация 11: путь хранится для admin-записи.
	openingsPath := resolveConfigPath("INTRO_OPENINGS_FILE", "config/openings.json")
	var openings *interviewer.Openings
	if o, loadErr := interviewer.LoadOpenings(openingsPath); loadErr != nil {
		log.Error("openings: файл набора начал не загружен — дефолтное начало", "err", loadErr)
		openings = nil
	} else {
		openings = o
	}
	// F7 (итерация 12): реестр tts-voices создаём ДО interviewer — резолвер рода
	// (speaker→gender) читает текущее значение; после admin-Reload род обновляется без
	// рестарта (тот же объект реестра, что у admin-эндпоинтов).
	ttsVoices := newTTSVoiceRegistry()
	interviewer := interviewer.New(provider, sessions, log,
		interviewer.WithProfiles(profiles),
		interviewer.WithReports(reports),
		interviewer.WithOpenings(openings),
		// spec-18: треки для промптов и критериев отчёта.
		interviewer.WithTrackSource(func(trackID string) (*models.Track, error) {
			return tracksStore.Get(trackID)
		}),
		interviewer.WithGenderResolver(ttsVoices.GenderOf),
		interviewer.WithVacancyResolver(func(ctx context.Context, userID int64) string {
			if t, err := users.GetVacancy(ctx, userID); err == nil {
				return t
			}
			return ""
		}),
		// Резюме кандидата: блок профиля + уровень интервью («на шаг выше базы»).
		interviewer.WithResumeResolver(func(ctx context.Context, userID int64) string {
			if r, err := users.GetResume(ctx, userID); err == nil {
				return r.Prompt()
			}
			return ""
		}))
	voice := voicesvc.NewClient(cfg.VoiceURL)
	s := &Server{
		cfg:           cfg,
		users:         users,
		sessions:      sessions,
		submissions:   submissions,
		whiteboards:   whiteboards,
		reports:       reports,
		templates:     templates,
		profiles:      profiles,
		tracks:        tracksStore,
		wsTickets:     wsTickets,
		loginLimit:    loginLimit,
		authIPLimit:   authIPLimit,
		registerLimit: registerLimit,
		wsTicketLimit: wsTicketLimit,
		runLimit:      runLimit,
		ttsVoices:     ttsVoices,
		openingsPath:  openingsPath,
		engine:        engine,
		interviewer:   interviewer,
		voice:         voice,
		log:           log,
		wsSessions:    make(map[int64]*wsSession),
	}
	// DI-оркестратор голосового хода (A1): стабильные зависимости (engine,
	// interviewer, лог, колбэки событий/текста). Voice передаётся в методы как
	// параметр (s.orchVoice) — тесты подменяют s.voice после NewWithLLM.
	s.orch = voiceorchestrator.New(voiceorchestrator.Deps{
		Engine:              engine,
		Interviewer:         interviewer,
		Log:                 log,
		EventData:           s.eventData,
		SendInterviewerText: s.sendInterviewerText,
	})
	return s
}

// Engine — движок сессий (main: Stop при завершении).
func (s *Server) Engine() *session.Engine { return s.engine }
