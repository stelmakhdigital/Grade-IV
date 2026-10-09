package interviewer

// A/B-оценка промптов живым LLM (T-20261009121144). Опционально: запускается
// только с LLM_EVAL=1 (и LLM_EVAL_URL/LLM_EVAL_MODEL, по умолчанию узел
// 192.168.1.114:8000). Нормальный сьют (без env) не затрагивается.
//
// Методика: корпус сценариев (фиксированный вход) → генерация реплик A и B
// (temperature как в проде) → LLM-judge по 4 критериям 1–5.
// Результаты: таблица + raw-JSON в LLM_EVAL_OUT (по умолчанию /tmp/prompts-ab).

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

type abScenario struct {
	name    string
	grade   models.Grade
	stack   string
	stage   models.Stage
	userMsg string
	temp    float64
	voice   bool // A/B различаются (voice-стадия) — иначе A=B (контекст)
}

// abCorpus — 10 сценариев-промпт-срезков (фиксированный вход).
var abCorpus = []abScenario{
	{"greeting_junior", "junior", "Go", models.StageVoice, "Кандидат перешёл на эту стадию. Заведи её: представь формат и задай первый вопрос (или, для Live-Code, объяви задачу).", 0.7, true},
	{"junior_cs", "junior", "Go", models.StageVoice, "Здравствуйте. Меня зовут Алексей, я junior-разработчик, последний год писал на Java, в Go только пробую.", 0.7, true},
	{"junior_motivation", "junior", "Go", models.StageVoice, "Хочу расти в бэкенд и научиться писать более масштабируемый код.", 0.7, true},
	{"middle_followup", "middle", "Python", models.StageVoice, "Я бы добавил кэш в Redis на горячие ключи, чтобы разгрузить базу. TTL ставил бы в минуту.", 0.7, true},
	{"middle_algorithms", "middle", "Python", models.StageVoice, "Для поиска в отсортированном массиве я бы использовал бинарный поиск, сложность O(log n).", 0.7, true},
	{"senior_challenge", "senior", "Go", models.StageVoice, "Я бы разбил систему на микросервисы: API-гейтвей, доменные сервисы и Kafka между ними, чтобы масштабировать каждую часть отдельно.", 0.7, true},
	{"senior_nudge", "senior", "Go", models.StageVoice, "Кандидат молчит уже 20 секунд. Мягко заполни паузу: уточни, всё ли понятно, повтори последний вопрос или дай лёгкую подсказку (1–2 предложения).", 0.7, true},
	{"livecode_pass", "middle", "Python", models.StageLiveCode, "Кандидат запустил код. Результаты (JSON): {\"passed\":true,\"exit_code\":0,\"duration_ms\":120,\"tests_total\":5,\"tests_failed\":0}\nКандидат запустил код: тесты прошли успешно. Exit code: 0, время: 120 мс. Дай короткое ревью (2–4 предложения) и один follow-up вопрос.", 0.5, false},
	{"livecode_fail", "middle", "Python", models.StageLiveCode, "Кандидат запустил код. Результаты (JSON): {\"passed\":false,\"exit_code\":1,\"duration_ms\":340,\"tests_total\":5,\"tests_failed\":2}\nКандидат запустил код: тесты не пройдены (выпало 2 из 5). Exit code: 1, время: 340 мс. Дай короткое ревью (2–4 предложения) и один follow-up вопрос.", 0.5, false},
	{"design_review", "senior", "Go", models.StageDesign, "Кандидат представил схему System Design. Структура (JSON: блоки и связи): {\"blocks\":[\"client\",\"lb\",\"api\",\"db\",\"cache\"],\"edges\":[[\"client\",\"lb\"],[\"lb\",\"api\"],[\"api\",\"db\"],[\"api\",\"cache\"]]}\nОцени по рубрике: (1) покрытие (клиент, балансировка, сервисы, БД/кэш, очереди, мониторинг), (2) масштабируемость, (3) отказоустойчивость, (4) обоснование trade-offs. В конце задай один follow-up вопрос. Устно: схема выглядит простой, очереди и мониторинга нет.", 0.5, false},
}

const abJudgeSystem = `Ты — независимый оценщик реплик ИИ-интервьюера голосового сервиса мок-интервью.
Оцени ОДНУ реплику ИИ по 4 критериям, каждый по шкале 1–5 (5 — отлично):
1. naturalness — естественность живой русской речи (не канцелярит, не шаблон, нет «воды»);
2. grade_fit — релевантность уровню кандидата (грейд указан в контексте);
3. specificity — конкретность (по делу, нет общих фраз вроде «важно понимать»);
4. voice_fit — пригодность для озвучки TTS: можно прочитать вслух без потери смысла (нет списков, нумерации, кода, сокращений «т.д.», «т.п.»).
Ответь СТРОГО JSON без пояснений: {"naturalness":N,"grade_fit":N,"specificity":N,"voice_fit":N,"comment":"одно короткое предложение"}`

type abLLMClient struct {
	base   string
	model  string
	client *http.Client
}

// abChat — один chat-completion запрос (OpenAI-совместимый).
func (c *abLLMClient) chat(ctx context.Context, temp float64, msgs []map[string]string) (string, error) {
	// как prod-клиент (internal/llm): thinking-фазу отключаем (latency, контент сразу)
	body, _ := json.Marshal(map[string]any{
		"model":                c.model,
		"messages":             msgs,
		"temperature":          temp,
		"max_tokens":           300,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	})
	req, err := http.NewRequestWithContext(ctx, "POST", c.base+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return "", fmt.Errorf("http %d", resp.StatusCode)
	}
	var out struct {
		Choices []struct {
			Message struct{ Content string } `json:"message"`
		} `json:"choices"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("пустой ответ LLM")
	}
	return out.Choices[0].Message.Content, nil
}

func TestABEvalLLM(t *testing.T) {
	if os.Getenv("LLM_EVAL") == "" {
		t.Skip("LLM_EVAL не задан — опциональная A/B-оценка живым LLM (LLM_EVAL=1)")
	}
	base := os.Getenv("LLM_EVAL_URL")
	if base == "" {
		base = "http://192.168.1.114:8000/v1"
	}
	model := os.Getenv("LLM_EVAL_MODEL")
	if model == "" {
		model = "qwen3.8-27b-fp8"
	}
	outDir := os.Getenv("LLM_EVAL_OUT")
	if outDir == "" {
		outDir = "/tmp/prompts-ab"
	}
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Minute)
	defer cancel()
	c := &abLLMClient{base: base, model: model, client: &http.Client{Timeout: 120 * time.Second}}

	type row struct {
		Scenario  string         `json:"scenario"`
		Variant   string         `json:"variant"`
		Greyd     string         `json:"grade"`
		Stage     string         `json:"stage"`
		Text      string         `json:"text"`
		Scores    map[string]int `json:"scores"`
		Comment   string         `json:"comment"`
		Heuristic []string       `json:"heuristic_violations"`
	}
	var rows []row
	var md strings.Builder
	md.WriteString("| сценарий | вариант | naturalness | grade_fit | specificity | voice_fit | сумма | эвристика |\n")
	md.WriteString("|---|---|---|---|---|---|---|---|\n")

	for _, s := range abCorpus {
		variants := []string{"A=B"} // контекстные сценарии: A и B совпадают
		if s.voice {
			variants = []string{"A", "B"}
		}
		texts := map[string]string{}
		for _, v := range variants {
			style := voiceStyleA
			if v == "B" {
				style = voiceStyleB
			}
			sys := systemPrompt(s.grade, s.stack, s.stage, style)
			tCtx := ctx
			text, err := c.chat(tCtx, s.temp, []map[string]string{
				{"role": "system", "content": sys},
				{"role": "user", "content": s.userMsg},
			})
			if err != nil {
				t.Fatalf("генерация %s/%s: %v", s.name, v, err)
			}
			texts[v] = strings.TrimSpace(text)
		}
		for _, v := range variants {
			text := texts[v]
			// Эвристика (детерминированный слой): для voice — вопрос/длина/списки.
			wantQ := s.stage == models.StageVoice && !strings.Contains(s.userMsg, "молчит")
			heuristic := validateVoiceReply(t, text, wantQ)
			judge, err := c.chat(ctx, 0.2, []map[string]string{
				{"role": "system", "content": abJudgeSystem},
				{"role": "user", "content": fmt.Sprintf("Контекст: стадия %s, грейд %s, стек %s.\nРеплика кандидата (вход): %s\nРеплика ИИ (оцени): %s",
					s.stage, s.grade, s.stack, s.userMsg, text)},
			})
			if err != nil {
				t.Fatalf("judge %s/%s: %v", s.name, v, err)
			}
			var scores map[string]any
			if err := json.Unmarshal([]byte(extractJSON(judge)), &scores); err != nil {
				t.Fatalf("judge JSON %s/%s: %v (%q)", s.name, v, err, judge)
			}
			ii := map[string]int{}
			sum := 0
			for _, k := range []string{"naturalness", "grade_fit", "specificity", "voice_fit"} {
				n, _ := scores[k].(float64)
				ii[k] = int(n)
				sum += int(n)
			}
			comment, _ := scores["comment"].(string)
			rows = append(rows, row{s.name, v, string(s.grade), string(s.stage), text, ii, comment, heuristic})
			md.WriteString(fmt.Sprintf("| %s | %s | %d | %d | %d | %d | %d | %s |\n",
				s.name, v, ii["naturalness"], ii["grade_fit"], ii["specificity"], ii["voice_fit"], sum,
				strings.Join(heuristic, "; ")))
		}
	}
	raw, _ := json.MarshalIndent(rows, "", "  ")
	if err := os.WriteFile(filepath.Join(outDir, "raw.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "table.md"), []byte(md.String()), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Logf("A/B-оценка: %d строк → %s", len(rows), outDir)
	// Итог по voice-сценариям (A vs B).
	sumA, nA, sumB, nB := 0, 0, 0, 0
	for _, r := range rows {
		s := r.Scores["naturalness"] + r.Scores["grade_fit"] + r.Scores["specificity"] + r.Scores["voice_fit"]
		switch r.Variant {
		case "A":
			sumA += s
			nA++
		case "B":
			sumB += s
			nB++
		}
	}
	if nB > 0 {
		t.Logf("Средняя сумма (4 критерия, макс 20): A=%.1f (n=%d), B=%.1f (n=%d)", float64(sumA)/float64(nA), nA, float64(sumB)/float64(nB), nB)
	}
}

// extractJSON — первый {...} блок (ответ judge может содержать обвязку).
func extractJSON(s string) string {
	start := strings.Index(s, "{")
	end := strings.LastIndex(s, "}")
	if start < 0 || end <= start {
		return s
	}
	return s[start : end+1]
}
