package models

import (
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Track — профиль роли собеседования (spec-18): состав стадий, конфиг доски,
// критерии отчёта. Дефолтные треки — config/roles.json (оверрайд через
// ROLE_TRACKS_FILE), правка — админ-API /admin/tracks.
//
// Инварианты (Validate):
//   - id: ^[a-z0-9-]{2,32}$, title непустой;
//   - coding=true → stacks ⊆ {go, python}, непусто; coding=false → stacks пуст;
//   - grades ⊆ {junior, middle, senior, staff}, непусто;
//   - criteria: ≥1, веса в [0..1], сумма 0.95..1.05 (нормализуется при валидации);
//   - board всегда задан (доска — универсальная стадия «показать/объяснить»).
type Track struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Coding — трек с кодовой стадией (Live-Code go/python + sandbox). Для
	// некодовых ролей (PM) стадия livecode не входит в программу.
	Coding bool `json:"coding"`
	// Shell — практическая shell-стадия (spec-19): bash-задания из банка
	// sandbox (solution.sh + чек-скрипт), стадия livecode в редакторе.
	// Только для некодовых ролей (сисадмин): coding и shell — взаимоисключающие
	// практические стадии (одна стадия livecode, выбор задач — по треку).
	Shell bool `json:"shell,omitempty"`
	// Stacks — допустимые стеки кодовой стадии (для coding-треков).
	Stacks []Stack `json:"stacks,omitempty"`
	// Grades — допустимые грейды трека (выбор в кабинете фильтруется).
	Grades []Grade `json:"grades"`
	// Board — конфиг стадии доски (spec-18/Ф3): палитра, заголовок, промпт
	// ревью, кейс-задания по грейду.
	Board BoardConfig `json:"board"`
	// Criteria — критерии отчёта с весами (Ф2). Порядок = порядок в отчёте.
	Criteria []TrackCriterion `json:"criteria"`
	// GradeConfirm — порог подтверждения грейда (опциональный override
	// глобального 4.2). 0 — глобальный.
	GradeConfirm float64 `json:"grade_confirm,omitempty"`
}

// BoardConfig — конфигурация стадии доски (spec-18/Ф3).
type BoardConfig struct {
	// Title — заголовок доски («Схема инфраструктуры», «План проекта»).
	Title string `json:"title"`
	// ReviewPrompt — что оценивать в схеме (в system-промпт ревью доски).
	ReviewPrompt string `json:"review_prompt"`
	// Groups — палитра блоков по группам (рендерится в DesignPanel).
	Groups []BlockGroup `json:"groups"`
	// Cases — кейс-задания по грейду (Ф5): ИИ объявляет кейс на входе доски,
	// кандидат рисует и объясняет; чек-лист попадает в промпт ревью. Пусто —
	// доска без кейса (свободное задание из review_prompt).
	Cases []BoardCase `json:"cases,omitempty"`
}

// BlockGroup — группа палитры доски.
type BlockGroup struct {
	Title string   `json:"title"`
	Names []string `json:"names"`
	// Color — hex базового цвета блоков группы (#rrggbb); пусто — дефолтный
	// цвет по индексу группы.
	Color string `json:"color,omitempty"`
}

// BoardCase — кейс-задание стадии доски (spec-18/Ф5).
type BoardCase struct {
	Grade Grade `json:"grade"`
	// Title — короткое имя кейса («Миграция офисной сети»).
	Title string `json:"title"`
	// Statement — текст задания кандидату (озвучивает ИИ, висит на доске).
	Statement string `json:"statement"`
	// Checklist — чек-лист ожидаемых элементов решения (в промпт ревью и
	// критерий отчёта).
	Checklist []string `json:"checklist"`
}

// TrackCriterion — критерий отчёта трека (Ф2).
type TrackCriterion struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	// Hint — что именно оценивать (пояснение для LLM-оценщика).
	Hint string `json:"hint,omitempty"`
}

// DefaultTrackID — трек по умолчанию (совместимость: старые сессии/клиенты).
const DefaultTrackID = "backend"

var trackIDRe = regexp.MustCompile(`^[a-z0-9-]{2,32}$`)

// Validate — структурная валидация + нормализация весов критериев (сумма → 1).
func (t *Track) Validate() error {
	if !trackIDRe.MatchString(t.ID) {
		return fmt.Errorf("track id %q: строчные буквы/цифры/дефис, 2–32", t.ID)
	}
	if strings.TrimSpace(t.Title) == "" {
		return fmt.Errorf("track %s: пустой title", t.ID)
	}
	if len(t.Grades) == 0 {
		return fmt.Errorf("track %s: grades пуст", t.ID)
	}
	for _, g := range t.Grades {
		if !g.Valid() {
			return fmt.Errorf("track %s: грейд %q", t.ID, g)
		}
	}
	if t.Coding {
		if len(t.Stacks) == 0 {
			return fmt.Errorf("track %s: coding без стеков", t.ID)
		}
		for _, s := range t.Stacks {
			if !s.Valid() {
				return fmt.Errorf("track %s: стек %q", t.ID, s)
			}
		}
	} else if len(t.Stacks) != 0 {
		return fmt.Errorf("track %s: некодовый трек со стеками", t.ID)
	}
	if t.Shell && t.Coding {
		return fmt.Errorf("track %s: coding и shell взаимоисключающие (одна практическая стадия)", t.ID)
	}
	if len(t.Criteria) == 0 {
		return fmt.Errorf("track %s: критерии пусты", t.ID)
	}
	sum := 0.0
	for i, c := range t.Criteria {
		if strings.TrimSpace(c.Name) == "" {
			return fmt.Errorf("track %s: критерий %d без имени", t.ID, i)
		}
		if c.Weight < 0 || c.Weight > 1 {
			return fmt.Errorf("track %s: вес критерия %q вне [0..1]", t.ID, c.Name)
		}
		sum += c.Weight
	}
	if sum < 0.95 || sum > 1.05 {
		return fmt.Errorf("track %s: сумма весов %.3f не в 0.95..1.05", t.ID, sum)
	}
	// Нормализация (best practice: храним нормализованное, чтобы UI/LLM
	// всегда видели доли).
	fix := 1.0 / sum
	for i := range t.Criteria {
		t.Criteria[i].Weight = t.Criteria[i].Weight * fix
	}
	if strings.TrimSpace(t.Board.Title) == "" {
		return fmt.Errorf("track %s: board.title пуст", t.ID)
	}
	if len(t.Board.Groups) == 0 {
		return fmt.Errorf("track %s: палитра доски пуста", t.ID)
	}
	for gi, g := range t.Board.Groups {
		if strings.TrimSpace(g.Title) == "" || len(g.Names) == 0 {
			return fmt.Errorf("track %s: группа палитры %d пуста", t.ID, gi)
		}
	}
	for i, c := range t.Board.Cases {
		if strings.TrimSpace(c.Title) == "" || strings.TrimSpace(c.Statement) == "" {
			return fmt.Errorf("track %s: кейс %d без title/statement", t.ID, i)
		}
	}
	if t.GradeConfirm != 0 && (t.GradeConfirm < 1 || t.GradeConfirm > 5) {
		return fmt.Errorf("track %s: grade_confirm %.2f вне [1..5]", t.ID, t.GradeConfirm)
	}
	return nil
}

// HasGrade — грейд допустим в треке.
func (t *Track) HasGrade(g Grade) bool {
	for _, x := range t.Grades {
		if x == g {
			return true
		}
	}
	return false
}

// HasStack — стек допустим в треке (coding=true).
func (t *Track) HasStack(s Stack) bool {
	for _, x := range t.Stacks {
		if x == s {
			return true
		}
	}
	return false
}

// CaseForGrade — кейс доски для грейда (первый совпавший; пусто — без кейса).
func (b *BoardConfig) CaseForGrade(g Grade) *BoardCase {
	for i := range b.Cases {
		if b.Cases[i].Grade == g {
			return &b.Cases[i]
		}
	}
	return nil
}

// SortCriteria — детерминированный порядок критериев (по весу убыв., затем имя).
func (t *Track) SortCriteria() {
	sort.SliceStable(t.Criteria, func(i, j int) bool {
		if t.Criteria[i].Weight != t.Criteria[j].Weight {
			return t.Criteria[i].Weight > t.Criteria[j].Weight
		}
		return t.Criteria[i].Name < t.Criteria[j].Name
	})
}

// Stages — программа стадий трека для грейда (spec-18/Ф1).
// Coding: junior без design (историческое правило грейда); остальные — полный
// набор voice → livecode → design → report.
// Non-coding: voice → design (доска «показать/объяснить») → report — Live-Code
// не входит, доска обязательна для всех грейдов.
func (t *Track) Stages(g Grade) []Stage {
	if t.Coding {
		if g == GradeJunior {
			return []Stage{StageVoice, StageLiveCode, StageReport}
		}
		return []Stage{StageVoice, StageLiveCode, StageDesign, StageReport}
	}
	if t.Shell {
		// spec-19: практическая shell-стадия для некодовых ролей (сисадмин):
		// голос → bash-задание в редакторе → доска (архитектура) → отчёт.
		return []Stage{StageVoice, StageLiveCode, StageDesign, StageReport}
	}
	return []Stage{StageVoice, StageDesign, StageReport}
}

// DefaultTrackIDIf — trackID трека или 'backend' (nil → дефолт совместимости).
// ShellStage — имя практической shell-стадии для UI-навигации.
const ShellStage = "shell"

func DefaultTrackIDIf(t *Track) string {
	if t == nil {
		return DefaultTrackID
	}
	return t.ID
}
