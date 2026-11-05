package models

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// Track — роль собеседования (spec-22, редизайн spec-18): описание роли,
// опциональная практическая стадия, конфиг доски, критерии отчёта. Роль
// самодостаточна: в кабинете выбирают только роль и грейд — стек практики
// зашит в роль (админ создаёт роль под язык; выбор стека кандидатом убран).
//
// Дефолтные роли — config/roles.json (оверрайд ROLE_TRACKS_FILE), правка —
// админ-API /admin/tracks, черновик — POST /admin/tracks/generate (ИИ
// заполняет конфиг по короткому описанию).
//
// Инварианты (Validate):
//   - id: ^[a-z0-9-]{2,32}$, title непустой;
//   - practice.runtime ∈ {go, python, shell} (банков задач и рантаймов
//     sandbox — только эти три);
//   - grades ⊆ {junior, middle, senior, staff}, непусто;
//   - criteria: ≥1, веса в [0..1], сумма 0.95..1.05 (нормализуется);
//   - board всегда задан (доска — универсальная стадия «показать/объяснить»).
type Track struct {
	ID string `json:"id"`
	// Title — человекочитаемое имя роли («Backend-разработчик»).
	Title string `json:"title"`
	// Description — 1–3 предложения: что за роль и что проверяем. Видят
	// кандидату? Нет: для админа и ИИ-заполнения/оценщика.
	Description string `json:"description,omitempty"`
	// Practice — практическая стадия в редакторе (nil = без практики: голос +
	// доска). Один runtime на роль: банк задач и рантайм sandbox выбираются
	// по нему; «стеки на выбор кандидата» убраны (spec-22).
	Practice *Practice `json:"practice,omitempty"`
	// Grades — допустимые грейды роли (выбор в кабинете фильтруется).
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

// Practice — практическая стадия роли (spec-22).
type Practice struct {
	// Runtime — чем решается практика: go | python | shell (bash). Определяет
	// банк задач sandbox и рантайм прогона; подпись стадии («Практика (Go)»)
	// строится из него.
	Runtime Stack `json:"runtime"`
}

// trackLegacy — поля трека до spec-22 (coding/shell/stacks): читаются для
// совместимости, чтобы треки, сохранённые админом в старой форме, не теряли
// практическую стадию.
type trackLegacy struct {
	Coding bool    `json:"coding"`
	Shell  bool    `json:"shell"`
	Stacks []Stack `json:"stacks"`
}

// UnmarshalJSON — совместимость: старая форма (coding/shell/stacks) конвертируется
// в practice (первый стек кодовой стадии; shell-флаг → bash). Новая форма — как есть.
func (t *Track) UnmarshalJSON(b []byte) error {
	type rawTrack Track // без методов — рекурсии нет
	var r rawTrack
	if err := json.Unmarshal(b, &r); err != nil {
		return err
	}
	*t = Track(r)
	var lg trackLegacy
	if err := json.Unmarshal(b, &lg); err == nil {
		if t.Practice == nil {
			switch {
			case lg.Coding && len(lg.Stacks) > 0:
				t.Practice = &Practice{Runtime: lg.Stacks[0]}
			case lg.Shell:
				t.Practice = &Practice{Runtime: StackShell}
			}
		}
	}
	return nil
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

// TrackCriterion — критерий отчёта роли (Ф2).
type TrackCriterion struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
	// Hint — что именно оценивать (пояснение для LLM-оценщика).
	Hint string `json:"hint,omitempty"`
}

// DefaultTrackID — роль по умолчанию (совместимость: старые сессии/клиенты).
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
	if t.Practice != nil {
		if !t.Practice.Runtime.Valid() {
			return fmt.Errorf("track %s: рантайм практики %q (go/python/shell)", t.ID, t.Practice.Runtime)
		}
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

// HasGrade — грейд допустим в роли.
func (t *Track) HasGrade(g Grade) bool {
	for _, x := range t.Grades {
		if x == g {
			return true
		}
	}
	return false
}

// Runtime — рантайм практической стадии ("" — практики нет).
func (t *Track) Runtime() Stack {
	if t.Practice == nil {
		return ""
	}
	return t.Practice.Runtime
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

// Stages — программа стадий роли для грейда (spec-22): с практикой —
// голос → практика → доска → отчёт (у всех грейдов, включая junior —
// грейд-правило «junior без design» упразднено, см. BUG-20261104-026);
// без практики — голос → доска → отчёт.
func (t *Track) Stages(g Grade) []Stage {
	if t.Practice != nil {
		return []Stage{StageVoice, StageLiveCode, StageDesign, StageReport}
	}
	return []Stage{StageVoice, StageDesign, StageReport}
}

// StageMeta — метаданные роли для stage-сообщения: UI строит подписи
// («Практика (Go)») и навигацию стадий из данных. practice=null — роль без
// практики. Поле stages — программа грейда.
func (t *Track) StageMeta(g Grade) map[string]any {
	stages := make([]string, 0, 4)
	for _, s := range t.Stages(g) {
		stages = append(stages, string(s))
	}
	var pr any
	if t.Practice != nil {
		pr = map[string]any{"runtime": string(t.Practice.Runtime)}
	}
	return map[string]any{
		"id": t.ID, "title": t.Title, "practice": pr, "stages": stages,
	}
}

func DefaultTrackIDIf(t *Track) string {
	if t == nil {
		return DefaultTrackID
	}
	return t.ID
}
