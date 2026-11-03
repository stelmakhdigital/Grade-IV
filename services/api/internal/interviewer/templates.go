package interviewer

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// blockRe — блок программы: "(N) Название: описание".
var blockRe = regexp.MustCompile(`\((\d+)\)\s*([^:;]+?):\s*([^;]+?)\s*;`)

// parseProgramBlocks — разбор gradeProgram в блоки шаблона.
// Формат gradeProgram: "Программа ...: (1) X: y; (2) Z: w; ...".
func parseProgramBlocks(program string) []db.TemplateBlock {
	var blocks []db.TemplateBlock
	for _, m := range blockRe.FindAllStringSubmatch(program, -1) {
		n, _ := strconv.Atoi(m[1])
		blocks = append(blocks, db.TemplateBlock{
			Title:         strings.TrimSpace(m[2]),
			Focus:         strings.TrimSpace(m[3]),
			QuestionCount: n,
		})
	}
	if len(blocks) == 0 {
		blocks = []db.TemplateBlock{{Title: "Интервью", Focus: program, QuestionCount: 1}}
	}
	return blocks
}

// DefaultPrograms — дефолтные шаблоны (по одному на грейд) для сидинга
// таблицы interview_templates. Строятся из gradeProgram/gradeFocus.
// Названия: "Стандартное <грейд>" — дефолт, is_default=true, stack="*".
func DefaultPrograms() []db.Template {
	grades := []struct {
		grade models.Grade
		name  string
	}{
		{models.GradeJunior, "Стандартное Junior"},
		{models.GradeMiddle, "Стандартное Middle"},
		{models.GradeSenior, "Стандартное Senior"},
		{models.GradeStaff, "Стандартное Staff"},
	}
	out := make([]db.Template, 0, len(grades))
	for _, g := range grades {
		out = append(out, db.Template{
			Name:      g.name,
			Grade:     g.grade,
			Stack:     "*",
			DurationS: models.SessionDurationS(g.grade),
			Blocks:    parseProgramBlocks(ProgramFor(g.grade)),
			IsDefault: true,
		})
	}
	return append(out, trackPrograms()...)
}

// Track-константы для сидинга программ некодовых ролей (spec-18); дублируют
// id из config/roles.json (interviewer не зависит от tracks).
const (
	TrackSysadmin = "sysadmin"
	TrackPM       = "pm"
)

// DefaultProgramName — имя дефолтного шаблона грейда.
func DefaultProgramName(grade models.Grade) string {
	return fmt.Sprintf("Стандартное %s", strings.ToUpper(string(grade[:1]))+string(grade[1:]))
}

// trackPrograms — дефолтные программы голосовой стадии некодовых треков
// (spec-18): вопросы по домену роли вместо Go/Python deep-dive. Сидируются
// EnsureDefaults в scope (grade, track).
func trackPrograms() []db.Template {
	type tp struct {
		track  string
		grade  models.Grade
		name   string
		blocks []db.TemplateBlock
	}
	sysadmin := []tp{
		{TrackSysadmin, models.GradeJunior, "Сисадмин Junior", []db.TemplateBlock{
			{Title: "База: ОС и службы", Focus: "установка/обновление ОС, службы и логи, права доступа, базовые команды", QuestionCount: 2},
			{Title: "Сети: основы", Focus: "IP/маски, DHCP/DNS, маршрутизация, диагностика связности (ping/traceroute/tcpdump)", QuestionCount: 2},
			{Title: "Эксплуатация", Focus: "бэкапы (что/куда/как часто), мониторинг, порядок действий при инциденте", QuestionCount: 1},
			{Title: "Практика и мотивация", Focus: "разбор одного реального случая кандидата, чему научился", QuestionCount: 1},
		}},
		// Вычитка spec-19: middle — 6 вопросов (было 8, цель 6–8): глубже
		// сети и автоматизация, отказоустойчивость и поведение — по одному.
		{TrackSysadmin, models.GradeMiddle, "Сисадмин Middle", []db.TemplateBlock{
			{Title: "Сети и сегментация", Focus: "VLAN, файрволы, VPN, границы доверия, диагностика сложных кейсов", QuestionCount: 2},
			{Title: "Автоматизация", Focus: "скрипты (bash/python), управление конфигурациями (Ansible), IaC-подходы", QuestionCount: 2},
			{Title: "Отказоустойчивость", Focus: "RAID/репликация, кластеры, план восстановления после сбоя (DR)", QuestionCount: 1},
			{Title: "Поведенческое", Focus: "инцидент с последствиями, приоритизация заявок, документация", QuestionCount: 1},
		}},
		{TrackSysadmin, models.GradeSenior, "Сисадмин Senior", []db.TemplateBlock{
			{Title: "Архитектура инфраструктуры", Focus: "проектирование сегментов, масштабирование, цена решений", QuestionCount: 2},
			{Title: "Автоматизация и IaC", Focus: "полная воспроизводимость, версии, тестирование изменений", QuestionCount: 2},
			{Title: "Безопасность", Focus: "принцип наименьших привилегий, аудит, реакция на инциденты", QuestionCount: 2},
			{Title: "Лидерство", Focus: "наставничество, регламенты, взаимодействие с разработкой", QuestionCount: 2},
		}},
	}
	pm := []tp{
		// Вычитка spec-19: junior — цель 4–6 вопросов (было 8): база и
		// разбор проекта по 2, планирование/коммуникация по 1.
		{TrackPM, models.GradeJunior, "ПМ Junior", []db.TemplateBlock{
			{Title: "База роли", Focus: "функции ПМ, артефакты (план, статус-отчёт), отличие от проджекта/программы", QuestionCount: 2},
			{Title: "Планирование", Focus: "декомпозиция задачи, оценка сроков, что делать при сдвиге", QuestionCount: 1},
			{Title: "Коммуникация", Focus: "статус-встречи, эскалация проблем, работа с ожиданиями заказчика", QuestionCount: 1},
			{Title: "Практика и мотивация", Focus: "разбор одного проекта кандидата: роль, результат, сложности", QuestionCount: 2},
		}},
		{TrackPM, models.GradeMiddle, "ПМ Middle", []db.TemplateBlock{
			{Title: "Управление сроками и объёмом", Focus: "приоритизация, scope creep, критический путь, буферы", QuestionCount: 2},
			{Title: "Риски", Focus: "карта рисков, митигация, план Б, ранние сигналы", QuestionCount: 2},
			{Title: "Команда", Focus: "мотивация, конфликты, работа с подрядчиками, распределение задач", QuestionCount: 2},
			{Title: "Метрики", Focus: "как меряет прогресс и успех проекта, отчётность для руководства", QuestionCount: 2},
		}},
		{TrackPM, models.GradeSenior, "ПМ Senior", []db.TemplateBlock{
			{Title: "Сложные проекты", Focus: "несколько команд, зависимые проекты, бюджет и trade-offs", QuestionCount: 2},
			{Title: "Стейкхолдеры", Focus: "переговоры, изменение скоупа, конфликт интересов, эскалации", QuestionCount: 2},
			{Title: "Процессы", Focus: "выбор методологии (waterfall/agile-гибриды), внедрение изменений", QuestionCount: 2},
			{Title: "Лидерство", Focus: "развитие ПМ-команды, наставничество, ответственность за результат", QuestionCount: 2},
		}},
	}
	out := make([]db.Template, 0, len(sysadmin)+len(pm))
	for _, t := range append(sysadmin, pm...) {
		out = append(out, db.Template{
			Name:      t.name,
			Grade:     t.grade,
			Stack:     "*",
			Track:     t.track,
			DurationS: models.SessionDurationS(t.grade),
			Blocks:    t.blocks,
			IsDefault: true,
		})
	}
	return out
}
