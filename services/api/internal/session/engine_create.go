package session

import (
	"context"
	"fmt"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Создание сессии: опции Create (профиль, шаблон, трек, голос, грейд по резюме)
// и вставка записи со стартом в статусе active.

// CreateOption — опциональный параметр создания сессии.
type CreateOption func(*createParams)

type createParams struct {
	trackID    string
	templateID int64
	profileID  int64
	ttsspeaker string
	baseGrade  models.Grade
	// resumeTitle/resumeStacks — что ИИ вычитал из резюме (должность и языки).
	// Title идёт в заголовок интервью, stacks — в выбор кодовой стадии и в
	// директиву («спрашивать по всем заявленным»).
	resumeTitle  string
	resumeStacks []models.Stack
}

// WithProfile — профиль интервьюера (tone × difficulty, итерация B);
// влияет только на стиль, не на оценку.
func WithProfile(profileID int64) CreateOption {
	return func(p *createParams) { p.profileID = profileID }
}

// WithTemplate — шаблон интервью (итерация A); 0 — дефолтный шаблон грейда.
func WithTemplate(templateID int64) CreateOption {
	return func(p *createParams) { p.templateID = templateID }
}

// WithTrack — трек собеседования (spec-18); пусто — backend (дефолт).
func WithTrack(trackID string) CreateOption {
	return func(p *createParams) { p.trackID = trackID }
}

// WithTTSSpeaker — выбранный голос интервьюера (F1, Итерация 10); пусто — дефолт
// voice-сервиса (ru_01 → xenia).
func WithTTSSpeaker(speaker string) CreateOption {
	return func(p *createParams) { p.ttsspeaker = speaker }
}

// WithBaseGrade — грейд по резюме (ИИ определил его по резюме/описанию).
// Grade сессии при этом — цель: на шаг выше базы.
func WithBaseGrade(g models.Grade) CreateOption {
	return func(p *createParams) { p.baseGrade = g }
}

// WithResumeProfile — что ИИ вычитал из резюме: должность («Python-разработчик»,
// «Fullstack», «Системный администратор») и заявленные языки по убыванию веса.
// По первому из языков, который роль умеет, берётся кодовая стадия; остальные
// попадают в директиву интервьюера. Заголовок — должность из резюме, а не
// название роли: иначе Python-кандидат сидит в «Backend-разработчик» с go-задачами.
func WithResumeProfile(title string, stacks []models.Stack) CreateOption {
	return func(p *createParams) {
		p.resumeTitle = strings.TrimSpace(title)
		p.resumeStacks = stacks
	}
}

// Create создаёт сессию: вставка, старт в статусе active.
func (e *Engine) Create(ctx context.Context, userID int64, grade models.Grade, stack models.Stack, opts ...CreateOption) (models.Session, error) {
	var cp createParams
	for _, o := range opts {
		o(&cp)
	}
	return e.create(ctx, userID, grade, stack, &cp)
}

// CreateWithTemplate — создание сессии с шаблоном (итерация A).
// templateID=0 → дефолтный шаблон грейда (если templates задан); без templates
// — программа gradeProgram (как раньше). Program фиксируется в сессии.
func (e *Engine) CreateWithTemplate(ctx context.Context, userID int64, grade models.Grade, stack models.Stack, templateID int64) (models.Session, error) {
	return e.Create(ctx, userID, grade, stack, WithTemplate(templateID))
}

func (e *Engine) create(ctx context.Context, userID int64, grade models.Grade, stack models.Stack, cp *createParams) (models.Session, error) {
	templateID := cp.templateID
	// spec-18: трек сессии (пусто → backend; неизвестный id → 400).
	trackID := cp.trackID
	if trackID == "" {
		trackID = models.DefaultTrackID
	}
	var track *models.Track
	if e.tracks != nil {
		t, err := e.tracks(trackID)
		if err != nil {
			return models.Session{}, fmt.Errorf("%w: трек %q: %v", ErrInvalidParams, trackID, err)
		}
		if t == nil {
			return models.Session{}, fmt.Errorf("%w: трек %q не найден", ErrInvalidParams, trackID)
		}
		track = t
		if !track.HasGrade(grade) {
			return models.Session{}, fmt.Errorf("%w: грейд %q не входит в трек %q", ErrInvalidParams, grade, trackID)
		}
		// spec-22: стек практики — часть роли, клиентский stack не властен
		// ("" — без практики). Но роль умеет несколько языков (backend — go и
		// python), и внутри этого набора выбор обязан быть по резюме: пока роль
		// отдавала practice.runtime, Python-кандидат получал go-задачи только
		// потому, что в роли первым списан go (замер 2026-11-26: резюме на
		// FastAPI/asyncio, сессия — backend/go, отсюда и весь шум в оценках).
		stack = track.Runtime()
		if picked := pickResumeStack(cp.resumeStacks, track); picked != "" && picked != stack {
			e.log.Info("кодовая стадия — по резюме", "track", trackID,
				"resume_stacks", joinStacksForLog(cp.resumeStacks),
				"stack", picked, "стек_по_роли", string(stack))
			stack = picked
		} else if len(cp.resumeStacks) > 0 && picked == "" {
			e.log.Info("в резюме нет языка, который умеет роль — кодовая стадия по роли",
				"track", trackID, "resume_stacks", joinStacksForLog(cp.resumeStacks),
				"stack", string(stack))
		}
	}
	if !grade.Valid() {
		return models.Session{}, fmt.Errorf("%w: грейд %q", ErrInvalidParams, grade)
	}
	if track == nil {
		// Без резолвера треков (legacy-путь тестов/аварии) — старое правило.
		if !stack.Valid() {
			return models.Session{}, fmt.Errorf("%w: стек %q", ErrInvalidParams, stack)
		}
	}
	var err error
	now := e.now()
	limit := models.SessionDurationS(grade)
	switch {
	case e.sessionLimitS > 0:
		limit = e.sessionLimitS // SESSION_LIMIT_S — фиксированный лимит, с
	case e.sessionLimitS < 0:
		limit = 0 // SESSION_LIMIT_S=0/off — без ограничения по времени
	}
	// Шаблон: явный id или дефолтный грейда (итерация A).
	var program string
	if e.templates != nil {
		var tpl *db.Template
		if templateID > 0 {
			tpl, err = e.templates.Get(ctx, templateID)
			if err != nil {
				return models.Session{}, err
			}
			if tpl == nil || tpl.Grade != grade {
				return models.Session{}, fmt.Errorf("%w: шаблон %d не для грейда %s", ErrInvalidParams, templateID, grade)
			}
		} else {
			tpl, err = e.templates.Default(ctx, grade, string(stack), trackID)
			if err != nil {
				return models.Session{}, err
			}
		}
		if tpl != nil {
			program = tpl.ProgramString()
			templateID = tpl.ID
		}
	}
	if program == "" {
		// A6 (итерация 11): строки шаблона в БД нет → runtime использует
		// seed-программу из кода (gradeProgram в interviewer/prompts.go). WARN —
		// деградация к fallback (дефолты сидируются при Migrate, поэтому здесь —
		// только если шаблон грейда удалён/не создан).
		e.log.Warn("A6: строка шаблона в БД не найдена — используется seed-программа из кода (gradeProgram)",
			"grade", grade, "stack", stack, "template_id", templateID)
	}
	m, err := e.store.Create(ctx, models.Session{
		UserID:         userID,
		Grade:          grade,
		Stack:          stack,
		Stage:          models.StageVoice,
		Status:         models.StatusActive,
		DurationLimitS: limit,
		StartedAt:      now,
		TemplateID:     templateID,
		Program:        program,
		Stages:         stagesForTrack(track, grade), // issue #7: роль могут поправить во время сессии
		ProfileID:      cp.profileID,
		TTSSpeaker:     cp.ttsspeaker,
		TrackID:        trackID,
		BaseGrade:      cp.baseGrade,
		ResumeTitle:    cp.resumeTitle,
		ResumeStacks:   cp.resumeStacks,
	})
	if err != nil {
		return models.Session{}, err
	}
	e.mu.Lock()
	e.rts[m.ID] = &runtime{
		userID:      userID,
		grade:       grade,
		stack:       stack,
		track:       track,
		stages:      m.Stages,
		limitS:      m.DurationLimitS,
		stage:       models.StageVoice,
		status:      models.StatusActive,
		since:       now,
		lastPersist: now,
	}
	e.mu.Unlock()
	_, _ = e.addEvent(ctx, m.ID, "session_created", map[string]any{
		"grade": grade, "stack": stack, "duration_limit_s": m.DurationLimitS,
		"resume_title": cp.resumeTitle, "resume_stacks": cp.resumeStacks,
	})
	return m, nil
}

// pickResumeStack — первый заявленный по резюме язык, который роль умеет.
// Пустой ответ — ни один не подходит (кодовая стадия остаётся на рантайме роли).
func pickResumeStack(resume []models.Stack, track *models.Track) models.Stack {
	if track == nil {
		return ""
	}
	for _, s := range resume {
		if s.Valid() && track.HasStack(s) {
			return s
		}
	}
	return ""
}

// joinStacksForLog — стек в одну строку для лога.
func joinStacksForLog(st []models.Stack) string {
	names := make([]string, len(st))
	for i, s := range st {
		names[i] = string(s)
	}
	return strings.Join(names, ",")
}
