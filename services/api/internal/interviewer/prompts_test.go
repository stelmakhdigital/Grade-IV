package interviewer

import (
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// TestSystemPromptWithProfile — tone/difficulty попадают в system-промпт.
func TestSystemPromptWithProfile(t *testing.T) {
	toneWords := map[string]string{
		"strict":     "challenging follow-up",
		"balanced":   "Баланс поощрения и challenging",
		"supportive": "верное направление",
		"playful":    "Больше метафор, аналогий",
		"socratic":   "А как бы ты объяснил коллеге",
	}
	diffWords := map[string]string{
		"minus":    "Вопросы проще, чем типичный уровень грейда",
		"standard": "Вопросы по уровню грейда",
		"plus":     "Вопросы сложнее, чем типичный уровень грейда",
	}
	for tone, want := range toneWords {
		for diff, wantDiff := range diffWords {
			p := SystemPromptWithProfile(models.GradeMiddle, "go", models.StageVoice,
				"", tone, diff)
			if !strings.Contains(p, want) {
				t.Errorf("tone %q: промпт не содержит %q", tone, want)
			}
			if !strings.Contains(p, wantDiff) {
				t.Errorf("difficulty %q: промпт не содержит %q", diff, wantDiff)
			}
			if !strings.Contains(p, persona) {
				t.Errorf("tone %q: промпт без персоны", tone)
			}
		}
	}
}

// TestSystemPromptWithProfileUnknown — неизвестные tone/difficulty → default.
func TestSystemPromptWithProfileUnknown(t *testing.T) {
	grade, stack, stage := models.GradeSenior, "python", models.StageLiveCode
	defaultPrompt := SystemPromptWithProfile(grade, stack, stage, "", "balanced", "standard")
	got := SystemPromptWithProfile(grade, stack, stage, "", "aggressive", "nightmare")
	if got != defaultPrompt {
		t.Fatalf("неизвестные tone/difficulty: промпт отличается от default")
	}
}

// TestSystemPromptBackwardCompat — старый SystemPrompt = default-профиль.
func TestSystemPromptBackwardCompat(t *testing.T) {
	grade, stack, stage := models.GradeJunior, "go", models.StageVoice
	if got, want := SystemPrompt(grade, stack, stage),
		SystemPromptWithProfile(grade, stack, stage, activeVoiceStyle, "balanced", "standard"); got != want {
		t.Fatalf("SystemPrompt != SystemPromptWithProfile(default): backward-compat нарушен")
	}
	if !strings.Contains(SystemPrompt(grade, stack, stage), "Баланс поощрения и challenging") {
		t.Fatalf("SystemPrompt без balanced-блока")
	}
}

// Резюме в промпте: факты кандидата + уровень «на шаг выше базы» с шагом вниз.
func TestSystemPromptResumeBlock(t *testing.T) {
	with := SystemPromptWithProfileProgram(models.GradeSenior, "go", models.StageVoice,
		"balanced", "standard", "", "", "", "Иван, Go 5 лет", models.GradeMiddle, nil)
	for _, want := range []string{"Иван, Go 5 лет", "Резюме подтверждает уровень middle",
		"уровень senior", "переходи на уровень middle"} {
		if !strings.Contains(with, want) {
			t.Fatalf("в промпте нет %q:\n%s", want, with)
		}
	}
	// Без резюме или без базы по резюме — блока нет (промпт как раньше).
	noResume := SystemPromptWithProfileProgram(models.GradeSenior, "go", models.StageVoice,
		"balanced", "standard", "", "", "", "", models.GradeMiddle, nil)
	noBase := SystemPromptWithProfileProgram(models.GradeSenior, "go", models.StageVoice,
		"balanced", "standard", "", "", "", "Иван, Go 5 лет", "", nil)
	base := SystemPromptWithProfileProgram(models.GradeSenior, "go", models.StageVoice,
		"balanced", "standard", "", "", "", "", "", nil)
	if strings.Contains(noResume, "Резюме подтверждает") || strings.Contains(noBase, "Резюме подтверждает") {
		t.Fatal("блок резюме появился без резюме/базы")
	}
	if noResume != base || noBase != base {
		t.Fatal("промпт без блока резюме должен совпадать с обычным")
	}
}
