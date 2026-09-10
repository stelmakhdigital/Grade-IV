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

// Stack — стек программирования (MVP: go, python).
type Stack string

const (
	StackGo     Stack = "go"
	StackPython Stack = "python"
)

// Valid — верно, если стек в скоупе MVP.
func (s Stack) Valid() bool { return s == StackGo || s == StackPython }

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

// MinutesLedger — проводка учёта минут (+грант / −расход).
type MinutesLedger struct {
	ID           int64
	UserID       int64
	SessionID    *int64
	DeltaSeconds int
	Reason       string
	CreatedAt    time.Time
}
