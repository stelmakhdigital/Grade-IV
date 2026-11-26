package interviewer

import (
	"context"
	"log/slog"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Заявленные по резюме языки обязаны доходить до system-промпта. До этого
// модель видела только текст резюме и спрашивала по одному языку (обычно по
// первому встреченному): кандидат с go+python оставался без вопросов по
// второму. Строка ставится ПОСЛЕ обрезки резюме — иначе её отрезало бы на
// длинном резюме, и эффект пропал бы именно там, где он нужнее.
func TestResumeStacksReachSystemPrompt(t *testing.T) {
	long := strings.Repeat("Опыт: FastAPI, asyncio, PostgreSQL, Docker. ", 80)
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if err := db.Migrate(context.Background(), conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	// BaseGrade обязателен: блок резюме (и строка про языки внутри него)
	// попадает в промпт только когда база по резюме известна. Оба поля приходят
	// из одного разбора резюме, так что на реальном пути они вместе.
	i := New(llm.NewMockProvider(), db.NewSessionStore(conn, dialect), slog.New(slog.DiscardHandler),
		WithResumeResolver(func(context.Context, int64) string { return long }))

	sys := i.buildMessages(models.Session{
		ID: 1, UserID: 1, Grade: models.GradeSenior, BaseGrade: models.GradeMiddle,
		Stack: models.StackPython, Stage: models.StageVoice,
		ResumeStacks: []models.Stack{models.StackPython, models.StackGo},
	}, "Привет")[0].Content

	if !strings.Contains(sys, "Языки кандидата по резюме: python, go") {
		t.Fatalf("в system-промпте нет строки о заявленных языках:\n%s", sys[:min(len(sys), 800)])
	}
	if !strings.Contains(sys, "не только по первому") {
		t.Error("в строке нет указания спрашивать по каждому языку")
	}

	// Интервью не по резюме (языков нет) — лишней строки в промпте быть не должно.
	plain := i.buildMessages(models.Session{ID: 2, UserID: 1, Grade: models.GradeSenior,
		Stack: models.StackGo, Stage: models.StageVoice}, "Привет")[0].Content
	if strings.Contains(plain, "Языки кандидата по резюме") {
		t.Error("строка появилась там, где резюме не разбирали")
	}
}

func TestDeclaredStacksLineEmpty(t *testing.T) {
	if got := declaredStacksLine(nil); got != "" {
		t.Errorf("для пустого списка строка = %q", got)
	}
}
