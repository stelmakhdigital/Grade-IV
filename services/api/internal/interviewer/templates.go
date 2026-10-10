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
	return out
}

// DefaultProgramName — имя дефолтного шаблона грейда.
func DefaultProgramName(grade models.Grade) string {
	return fmt.Sprintf("Стандартное %s", strings.ToUpper(string(grade[:1]))+string(grade[1:]))
}
