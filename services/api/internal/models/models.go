// Package models — доменные модели «Грейд» (ARCHITECTURE.md §3).
package models

import "time"

// Grade — целевой грейд кандидата.
type Grade string

const (
	GradeJunior Grade = "junior"
	GradeMiddle Grade = "middle"
	GradeSenior Grade = "senior"
	GradeStaff  Grade = "staff"
)

// Valid — верно, если грейд известен.
func (g Grade) Valid() bool {
	switch g {
	case GradeJunior, GradeMiddle, GradeSenior, GradeStaff:
		return true
	}
	return false
}

// gradeOrder — грейды от младшего к старшему.
var gradeOrder = []Grade{GradeJunior, GradeMiddle, GradeSenior, GradeStaff}

// GradeRank — позиция грейда (0..3); неизвестный — как middle.
func GradeRank(g Grade) int {
	for i, x := range gradeOrder {
		if x == g {
			return i
		}
	}
	return 1
}

// GradeStep — грейд на n шагов от g (junior/staff — границы, дальше не растёт).
func GradeStep(g Grade, n int) Grade {
	i := GradeRank(g) + n
	if i < 0 {
		i = 0
	}
	if i >= len(gradeOrder) {
		i = len(gradeOrder) - 1
	}
	return gradeOrder[i]
}

// Stack — стек программирования (MVP: go, python).
type Stack string

const (
	StackGo     Stack = "go"
	StackPython Stack = "python"
	// StackShell — bash-практика (spec-19/22): solution.sh + чек-скрипт банка.
	StackShell Stack = "shell"
)

// Valid — верно, если стек в скоупе (go/python — код; shell — bash-практика).
func (s Stack) Valid() bool { return s == StackGo || s == StackPython || s == StackShell }

// Stage — стадия сессии.
type Stage string

const (
	StageVoice    Stage = "voice"
	StageLiveCode Stage = "livecode"
	StageDesign   Stage = "design"
	StageReport   Stage = "report"
)

// Status — статус сессии.
type Status string

const (
	StatusActive   Status = "active"
	StatusPaused   Status = "paused"
	StatusFinished Status = "finished"
	StatusAborted  Status = "aborted"
)

// SessionDurationS — длительность сессии по грейду, с (SRS US-2).
func SessionDurationS(g Grade) int {
	switch g {
	case GradeJunior:
		return 45 * 60
	case GradeMiddle:
		return 50 * 60
	case GradeSenior:
		return 60 * 60
	case GradeStaff:
		return 75 * 60
	default:
		return 60 * 60
	}
}

// User — пользователь (кандидат).
type User struct {
	ID           int64
	Email        string
	PasswordHash string
	CreatedAt    time.Time
	IsAdmin      bool // итерация 11: admin (первый зарегистрированный) — доступ к /api/v1/admin/*
}

// Session — сессия мок-интервью.
type Session struct {
	ID             int64
	UserID         int64
	Grade          Grade
	Stack          Stack
	Stage          Stage
	Status         Status
	DurationLimitS int
	ActiveSeconds  float64
	PausedAt       *time.Time
	StartedAt      time.Time
	FinishedAt     *time.Time
	TemplateID     int64  // NULL — дефолтный шаблон грейда (итерация A)
	Program        string // программа интервью из шаблона (в system-промпт)

	// Stages — программа стадий, закреплённая при создании сессии (колонка
	// sessions.stages, issue #7). Роль правят или удаляют во время интервью:
	// идущая сессия обязана жить по своей программе, иначе её текущая стадия
	// выпадает из программы и все переходы закрываются. Пусто у строк, созданных
	// до колонки: программа считается по текущей версии роли (как раньше).
	Stages []Stage

	// ProfileID — профиль интервьюера (tone × difficulty, итерация B);
	// 0 — default (balanced/standard). Влияет только на стиль, не на оценку.
	ProfileID int64

	// TTSSpeaker — выбранный голос интервьюера (F1, Итерация 10): спикер
	// voice-сервиса (aidar/eugene — male; baya/kseniya/xenia — female). Пусто —
	// дефолт voice-сервиса (ru_01 → xenia), backward-compat.
	TTSSpeaker string

	// TrackID — профиль роли собеседования (spec-18); 'backend' — дефолт
	// совместимости. Определяет состав стадий, доску и критерии отчёта.
	TrackID string

	// BaseGrade — грейд, который ИИ определил по резюме кандидата. Пусто — старт
	// без резюме (грейд выбран вручную). Grade при этом — цель: на шаг выше базы.
	BaseGrade Grade

	// ResumeTitle — должность как она написана в резюме («Python-разработчик»,
	// «Fullstack», «Системный администратор»). Заголовок интервью обязан
	// отражать резюме, а не название роли из config: Python-резюме, приведшее к
	// роли backend, не имеет права называться «Backend-разработчик».
	// Пусто — интервью не по резюме.
	ResumeTitle string

	// ResumeStacks — языки, которые кандидат заявил в резюме, по убыванию веса.
	// Stack сессии выбирается из них (если роль такой умеет), а остальные
	// попадают в директиву: спрашивать по всем заявленным, а не по одному.
	ResumeStacks []Stack

	// ShareToken — токен публичной ссылки на отчёт (F1-export, spec-16);
	// '' — ссылка не создана. GET /api/v1/public/reports/{token} без auth.
	ShareToken string
}

// SessionEvent — событие сессии (транскрипт, смена стадии и т.д.).
type SessionEvent struct {
	ID        int64
	SessionID int64
	Seq       int
	TS        time.Time
	Kind      string
	Data      []byte // JSON
}

// Submission — сдача кода кандидата на стадии Live-Code.
type Submission struct {
	ID         int64
	SessionID  int64
	TaskID     string
	Files      []byte // JSON: {path: content}
	Action     string
	ExitCode   *int
	Stdout     string
	Stderr     string
	DurationMS *int
	Tests      []byte // JSON: [{name, passed}]
	CreatedAt  time.Time
}

// Whiteboard — состояние холста System Design.
type Whiteboard struct {
	ID        int64
	SessionID int64
	State     []byte // JSON Excalidraw (elements + appState)
	Blocks    []byte // JSON извлечённой структуры блоков/связей
	PngPath   string
	UpdatedAt time.Time
}

// Report — итоговый отчёт (критерии — SRS §12).
type Report struct {
	ID                  int64
	SessionID           int64
	Overall             float64
	GradeRecommendation string
	Criteria            []byte // JSON: {критерий: балл}
	Strengths           []byte // JSON: []
	Weaknesses          []byte // JSON: []
	Recommendations     []byte // JSON: []
	CreatedAt           time.Time
}
