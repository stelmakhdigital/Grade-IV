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
