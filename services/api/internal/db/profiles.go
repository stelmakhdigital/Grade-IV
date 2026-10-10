package db

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"
)

// ErrProfileNotFound — профиль не найден.
var ErrProfileNotFound = errors.New("профиль не найден")

// Tone/difficulty (Итерация B): профиль влияет только на стиль вопросов,
// не на оценку (score по критериям §12).
const (
	ToneStrict     = "strict"
	ToneBalanced   = "balanced"
	ToneSupportive = "supportive"
	TonePlayful    = "playful"
	ToneSocratic   = "socratic"

	DifficultyMinus    = "minus"
	DifficultyStandard = "standard"
	DifficultyPlus     = "plus"
)

// validProfileValue — tone/difficulty из допустимого набора.
func validProfileValue(value, kind string) bool {
	switch kind {
	case "tone":
		switch value {
		case ToneStrict, ToneBalanced, ToneSupportive, TonePlayful, ToneSocratic:
			return true
		}
	case "difficulty":
		switch value {
		case DifficultyMinus, DifficultyStandard, DifficultyPlus:
			return true
		}
	}
	return false
}

// Profile — профиль интервьюера (таблица interviewer_profiles).
type Profile struct {
	ID          int64
	Name        string
	Tone        string
	Difficulty  string
	IsPreset    bool
	Description string
	CreatedAt   time.Time
}

// PresetProfiles — 10 пресетов (Итерация B, tone × difficulty).
func PresetProfiles() []Profile {
	return []Profile{
		{Name: "Строгий Senior", Tone: ToneStrict, Difficulty: DifficultyPlus,
			IsPreset: true, Description: "Короткие реплики, минимум хвалы, challenging follow-up"},
		{Name: "Сбалансированный наставник", Tone: ToneBalanced, Difficulty: DifficultyStandard,
			IsPreset: true, Description: "DEFAULT: баланс поощрения и challenging"},
		{Name: "Поддерживающий Junior", Tone: ToneSupportive, Difficulty: DifficultyMinus,
			IsPreset: true, Description: "Больше поощрения, подсказки раньше"},
		{Name: "Игривый коуч", Tone: TonePlayful, Difficulty: DifficultyStandard,
			IsPreset: true, Description: "Метафоры, аналогии, «расскажи как в команде»"},
		{Name: "Socratic Challenger", Tone: ToneSocratic, Difficulty: DifficultyPlus,
			IsPreset: true, Description: "Вопросы на вопросы, «а почему не X?», deep-dive"},
		{Name: "Строгий Middle", Tone: ToneStrict, Difficulty: DifficultyStandard,
			IsPreset: true, Description: "Точно по грейду, без отклонений"},
		{Name: "Мягкий Senior", Tone: ToneSupportive, Difficulty: DifficultyStandard,
			IsPreset: true, Description: "Поддержка + уровень senior"},
		{Name: "Игривый Junior", Tone: TonePlayful, Difficulty: DifficultyMinus,
			IsPreset: true, Description: "Для junior, лёгкий тон"},
		{Name: "Socratic Middle", Tone: ToneSocratic, Difficulty: DifficultyStandard,
			IsPreset: true, Description: "Вопросы на вопросы, middle-уровень"},
		{Name: "Строгий Staff", Tone: ToneStrict, Difficulty: DifficultyPlus,
			IsPreset: true, Description: "Для staff, максимум давления"},
	}
}

// ProfileStore — профили интервьюера (Итерация B).
type ProfileStore struct {
	db *sql.DB
	d  Dialect
}

// NewProfileStore создаёт хранилище.
func NewProfileStore(dbx *sql.DB, d Dialect) *ProfileStore {
	return &ProfileStore{db: dbx, d: d}
}

// Get — профиль по ID.
func (s *ProfileStore) Get(ctx context.Context, id int64) (Profile, error) {
	return s.scanProfile(s.db.QueryRowContext(ctx,
		s.d.q(`SELECT id, name, tone, difficulty, is_preset, description, created_at
			FROM interviewer_profiles WHERE id = ?`), id))
}

// List — все профили (пресеты первыми, по ID).
func (s *ProfileStore) List(ctx context.Context) ([]Profile, error) {
	rows, err := s.db.QueryContext(ctx,
		s.d.q(`SELECT id, name, tone, difficulty, is_preset, description, created_at
			FROM interviewer_profiles ORDER BY is_preset DESC, id`))
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()
	out := make([]Profile, 0, 16)
	for rows.Next() {
		p, err := s.scanProfile(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

// Create — новый пользовательский профиль (tone/difficulty валидируются).
func (s *ProfileStore) Create(ctx context.Context, p Profile) (Profile, error) {
	if p.Name == "" {
		return Profile{}, errors.New("пустое имя профиля")
	}
	if !validProfileValue(p.Tone, "tone") {
		return Profile{}, fmt.Errorf("недопустимый tone: %q", p.Tone)
	}
	if !validProfileValue(p.Difficulty, "difficulty") {
		return Profile{}, fmt.Errorf("недопустимая difficulty: %q", p.Difficulty)
	}
	res, err := s.db.ExecContext(ctx, s.d.q(`
		INSERT INTO interviewer_profiles (name, tone, difficulty, is_preset, description, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`),
		p.Name, p.Tone, p.Difficulty, p.IsPreset, p.Description, nowRFC3339())
	if err != nil {
		return Profile{}, fmt.Errorf("create profile: %w", err)
	}
	id, err := res.LastInsertId()
	if err != nil {
		return Profile{}, fmt.Errorf("last insert id: %w", err)
	}
	return s.Get(ctx, id)
}

// EnsurePresets — сид пресетов (идемпотентно): вставляет список, если таблица пуста.
func (s *ProfileStore) EnsurePresets(ctx context.Context, presets []Profile) error {
	var n int
	if err := s.db.QueryRowContext(ctx,
		s.d.q(`SELECT COUNT(*) FROM interviewer_profiles`)).Scan(&n); err != nil {
		return fmt.Errorf("count profiles: %w", err)
	}
	if n > 0 {
		return nil // уже сидированы (в т.ч. свои пресеты)
	}
	for _, p := range presets {
		if _, err := s.Create(ctx, p); err != nil {
			return fmt.Errorf("seed preset %q: %w", p.Name, err)
		}
	}
	return nil
}

func (s *ProfileStore) scanProfile(row rowScanner) (Profile, error) {
	var p Profile
	var createdAt string
	err := row.Scan(&p.ID, &p.Name, &p.Tone, &p.Difficulty, &p.IsPreset, &p.Description, &createdAt)
	if errors.Is(err, sql.ErrNoRows) {
		return Profile{}, ErrProfileNotFound
	}
	if err != nil {
		return Profile{}, fmt.Errorf("scan profile: %w", err)
	}
	p.CreatedAt, err = parseTS(createdAt)
	if err != nil {
		return Profile{}, fmt.Errorf("parse profile created_at: %w", err)
	}
	return p, nil
}
