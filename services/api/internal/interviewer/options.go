// Параметры движка интервьюера: опции WithXxx, структура Interviewer, треки,
// конструктор New и активный провайдер.
package interviewer

import (
	"context"
	"errors"
	"log/slog"
	"sync/atomic"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Ошибки.
var (
	ErrSessionEnded = errors.New("сессия завершена")
	ErrWrongStage   = errors.New("ход не соответствует стадии")
)

// Option — параметр движка.
type Option func(*Interviewer)

// WithMaxHistory — сколько последних реплик класть в контекст (default 20).
func WithMaxHistory(n int) Option {
	return func(i *Interviewer) {
		if n > 0 {
			i.maxHistory = n
		}
	}
}

// WithProfiles — профили интервьюера (Итерация B): tone/difficulty сессии
// попадают в system-промпт. Без опции — default (balanced/standard).
func WithProfiles(store *db.ProfileStore) Option {
	return func(i *Interviewer) { i.profiles = store }
}

// WithReports — хранилище отчётов (Итерация B): для progress vs прошлые интервью
// в GenerateReport. Без опции — без истории прогресса.
func WithReports(store *db.ReportStore) Option {
	return func(i *Interviewer) { i.reports = store }
}

// WithOpenings — настраиваемые случайные начала собеседования (F2, Итерация 10).
// Без опции (или пустой набор) — стартовое приветствие по-старому (backward-compat).
func WithOpenings(o *Openings) Option {
	return func(i *Interviewer) { i.openings.Store(o) }
}

// WithGenderResolver — резолвер speaker→gender (F7, итерация 12): род речи персоны
// по выбранному голосу. Источник — реестр tts-voices (httpapi, с Reload). Без опции
// / пустая строка — мужской (текущее поведение).
func WithGenderResolver(f func(speaker string) string) Option {
	return func(i *Interviewer) { i.genderResolver = f }
}

// WithVacancyResolver — резолвер userID→текст вакансии (F6, итерация 12): блок
// вакансии в system-промпт. Без опции / пустая строка — без блока вакансии.
func WithVacancyResolver(f func(ctx context.Context, userID int64) string) Option {
	return func(i *Interviewer) { i.vacancyResolver = f }
}

// WithResumeResolver — резолвер userID→резюме кандидата (описание + текст из файла):
// блок профиля и уровень интервью («на шаг выше базы») в system-промпт.
// Без опции / пустая строка — без блока, сложность строго по грейду сессии.
func WithResumeResolver(f func(ctx context.Context, userID int64) string) Option {
	return func(i *Interviewer) { i.resumeResolver = f }
}

// SetOpenings — замена набора начал вживую (итерация 11: admin-reload).
func (i *Interviewer) SetOpenings(o *Openings) { i.openings.Store(o) }

// CurrentOpenings — текущий набор начал (admin, итерация 11). Пусто — nil.
func (i *Interviewer) CurrentOpenings() []Opening {
	if op := i.openings.Load(); op != nil {
		return op.List()
	}
	return nil
}

// Interviewer — ходы ИИ-интервьюера.
type Interviewer struct {
	llm             llm.Provider
	sessions        *db.SessionStore
	profiles        *db.ProfileStore
	reports         *db.ReportStore
	openings        atomic.Pointer[Openings] // F2: случайные начала (настраиваемый набор); итерация 11 — atomic swap при admin-reload
	log             *slog.Logger
	maxHistory      int
	genderResolver  func(speaker string) string                    // F7: speaker→gender (реестр tts-voices); пусто/nil → male
	vacancyResolver func(ctx context.Context, userID int64) string // F6: userID→текст вакансии; пусто/nil → без блока
	resumeResolver  func(ctx context.Context, userID int64) string // userID→резюме (описание + файл); пусто/nil → без блока
	tracks          TrackSource                                    // spec-18: источник треков; nil → хардкод-критерии/промпты
}

// TrackSource — источник треков собеседований (spec-18): trackID → трек
// (nil, nil — не найден). nil-источник → fallback на хардкод грейда.
type TrackSource func(trackID string) (*models.Track, error)

// WithTrackSource — резолвер треков (spec-18).
func WithTrackSource(f TrackSource) Option {
	return func(i *Interviewer) { i.tracks = f }
}

// trackFor — трек сессии (spec-18). nil — источник не задан, id пустой или
// неизвестный: промпты/критерии — по грейду (совместимость).
func (i *Interviewer) trackFor(sess models.Session) *models.Track {
	if i.tracks == nil || sess.TrackID == "" {
		return nil
	}
	t, err := i.tracks(sess.TrackID)
	if err != nil || t == nil {
		i.log.Warn("interviewer: трек не разрешён — fallback на грейд",
			"session", sess.ID, "track", sess.TrackID, "err", err)
		return nil
	}
	return t
}

// New создаёт движок интервьюера.
func New(provider llm.Provider, sessions *db.SessionStore, log *slog.Logger, opts ...Option) *Interviewer {
	i := &Interviewer{llm: provider, sessions: sessions, log: log, maxHistory: 20}
	for _, opt := range opts {
		opt(i)
	}
	return i
}

// Provider — активный LLM-провайдер (health/тесты).
func (i *Interviewer) Provider() llm.Provider { return i.llm }
