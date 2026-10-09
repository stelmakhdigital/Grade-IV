package interviewer

import (
	"fmt"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// A/B-промпты (T-20261009121144): детерминированные юнит-критерии на уровне
// промпт-контракта (что просит промпт) + эвристика валидации реплик (для
// smoke на mock-LLM и для проверки данных LLM-оценки). Без живого LLM.

// wordCount — число слов (пробельная разбивка).
func wordCount(s string) int {
	return len(strings.Fields(s))
}

// listMarkers — маркеры списков/нумерации, запрещённые в голосовой реплике.
var listMarkers = []string{"1)", "2)", "3)", "1.", "2.", "3.", "•"}

// validateVoiceReply — эвристика голосовой реплики: возвращает список
// нарушений (пусто — реплика подходит для TTS). wantQuestion — в реплике
// должен быть вопрос (voice-стадия).
func validateVoiceReply(t *testing.T, text string, wantQuestion bool) []string {
	t.Helper()
	var v []string
	if n := wordCount(text); n > 40 {
		v = append(v, fmt.Sprintf("длина: %d слов > 40", n))
	}
	for _, m := range listMarkers {
		if strings.Contains(text, m) {
			v = append(v, fmt.Sprintf("список/нумерация: %q", m))
			break
		}
	}
	for _, abbr := range []string{"т.д.", "т.п.", "и т.д."} {
		if strings.Contains(strings.ToLower(text), abbr) {
			v = append(v, fmt.Sprintf("сокращение: %q", abbr))
		}
	}
	if wantQuestion && !strings.Contains(text, "?") {
		v = append(v, "нет вопроса (нет «?»)")
	}
	return v
}

// TestPromptABContract — промпт-контракт: A (текущий) vs B (формат-блок).
// Не меняются: персона, фокус/программа грейда, другие стадии.
func TestPromptABContract(t *testing.T) {
	a := systemPrompt(models.GradeJunior, "Go", models.StageVoice, voiceStyleA)
	b := systemPrompt(models.GradeJunior, "Go", models.StageVoice, voiceStyleB)
	if !strings.Contains(b, "Одна мысль за раз. "+voiceStyleB) {
		t.Fatal("B: формат-блок не вставлен после «Одна мысль за раз.»")
	}
	if strings.Contains(a, "40 слов") {
		t.Fatal("A: не должно содержать ограничение «40 слов»")
	}
	// B: контракт формата (что просит промпт).
	for _, want := range []string{"1–2 предложения", "40 слов", "один вопрос ИЛИ", "без списков", "давайте разберём"} {
		if !strings.Contains(b, want) {
			t.Fatalf("B: нет контракта %q", want)
		}
	}
	// Не изменено: персона и программа грейда одинаковы в A и B.
	for _, want := range []string{"ИИ-интервьюер «Грейд»", "Программа голосового интервью (Junior"} {
		if !strings.Contains(a, want) || !strings.Contains(b, want) {
			t.Fatalf("персона/программа грейда не сохранена (%q): A=%v B=%v", want, strings.Contains(a, want), strings.Contains(b, want))
		}
	}
	// Другие стадии — без изменений.
	for _, stage := range []models.Stage{models.StageLiveCode, models.StageDesign} {
		if sa := systemPrompt(models.GradeSenior, "Python", stage, voiceStyleA); sa != systemPrompt(models.GradeSenior, "Python", stage, voiceStyleB) {
			t.Fatalf("стдия %v: A/B должны совпадать", stage)
		}
	}
	// Публичный SystemPrompt = активный вариант.
	if SystemPrompt(models.GradeMiddle, "Go", models.StageVoice) != systemPrompt(models.GradeMiddle, "Go", models.StageVoice, activeVoiceStyle) {
		t.Fatal("SystemPrompt != systemPrompt(активный вариант)")
	}
}

// TestVoiceReplyHeuristic — эвристика валидации реплик (smoke без LLM).
func TestVoiceReplyHeuristic(t *testing.T) {
	// Хорошая реплика (вопрос, 1 предложение, без списков).
	good := "Расскажите, как вы бы оценили сложность вашей функции и где узкое место?"
	if v := validateVoiceReply(t, good, true); len(v) != 0 {
		t.Fatalf("хорошая реплика: нарушения %v", v)
	}
	// Хорошая реплика-подсказка (без вопроса).
	good2 := "Подсказка: начните с того, как данные проходят через ваш сервис."
	if v := validateVoiceReply(t, good2, false); len(v) != 0 {
		t.Fatalf("хорошая подсказка: нарушения %v", v)
	}
	// Длинная реплика (> 40 слов).
	long := strings.Repeat("слово ", 45)
	if v := validateVoiceReply(t, long, false); len(v) == 0 {
		t.Fatal("длинная реплика (45 слов) не помечена")
	}
	// Список/нумерация.
	list := "Посмотрим так: 1) база данных, 2) кэш, 3) балансировщик. С чего начнём?"
	if v := validateVoiceReply(t, list, true); len(v) == 0 {
		t.Fatal("список «1) 2) 3)» не помечен")
	}
	// Сокращение «т.д.»
	abbr := "Обычно это горутины, каналы, контекст, т.д. Что из этого вы использовали?"
	if v := validateVoiceReply(t, abbr, true); len(v) == 0 {
		t.Fatal("сокращение «т.д.» не помечено")
	}
	// Нет вопроса, когда ожидается.
	noQ := "Я вижу, что вы описали подход, но деталей не хватает."
	if v := validateVoiceReply(t, noQ, true); len(v) == 0 {
		t.Fatal("отсутствие вопроса не помечено")
	}
}

// TestPromptSmokeMock — smoke: полный промпт собирается, не пуст, содержит
// персона/грейд/стадию (контракт для всех грейдов).
func TestPromptSmokeMock(t *testing.T) {
	for _, grade := range []models.Grade{"junior", "middle", "senior", "staff"} {
		for _, style := range []string{voiceStyleA, voiceStyleB} {
			p := systemPrompt(grade, "Go", models.StageVoice, style)
			if len(p) < 200 {
				t.Fatalf("грейд %v: промпт подозрительно короткий (%d)", grade, len(p))
			}
			if !strings.Contains(p, "ИИ-интервьюер «Грейд»") || !strings.Contains(p, "Стадия: голосовое интервью") {
				t.Fatalf("грейд %v: нет персона/стадии", grade)
			}
			if !strings.Contains(p, "Программа голосового интервью") {
				t.Fatalf("грейд %v: нет программы", grade)
			}
		}
	}
}

// TestCodeRunHintContract — контракт code-review-хинта (длина + follow-up).
func TestCodeRunHintContract(t *testing.T) {
	for _, passed := range []bool{true, false} {
		h := CodeRunHint(passed, 0, 120, 5, 1)
		if !strings.Contains(h, "2–4 предложения") || !strings.Contains(h, "follow-up") {
			t.Fatalf("runReviewHint(passed=%v): нет контракта длины/follow-up: %q", passed, h)
		}
	}
}
