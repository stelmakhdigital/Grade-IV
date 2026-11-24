package taskpool

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// LLM — минимум, нужный пулу (реализация — llm.Provider: и настоящий клиент,
// и мок из тестов).
type LLM interface {
	Chat(ctx context.Context, req llm.Request) (llm.Response, error)
	Name() string
}

// bankTask — задача из банка вместе с мета-данными генерации: для «кожи» нужны
// семья и seed, из которых sandbox соберёт файлы заново.
type bankTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Statement string `json:"statement"`
	Meta      *struct {
		Family    string `json:"family"`
		Seed      int64  `json:"seed"`
		Generator string `json:"generator"`
	} `json:"meta"`
}

// skinTick — уровень B (spec-24): LLM переписывает условие семьи под домен роли.
// Файлы и проверки не трогаются: их собирает семья по seed, а gate прогоняет
// заново. Меняется только проза — потому что проза не исполняется.
//
// Одна попытка за тик: ответ модели — секунды, а пул работа фоновая.
func (j *Job) skinTick(ctx context.Context) {
	stack := j.cfg.Stacks[j.cfg.Rand.Intn(len(j.cfg.Stacks))]
	grade := j.cfg.Grades[j.cfg.Rand.Intn(len(j.cfg.Grades))]
	var list []bankTask
	if err := j.getJSON(ctx, j.cfg.SandboxURL+"/api/v1/tasks?stack="+stack+"&grade="+grade, nil, &list); err != nil {
		j.log.Warn("taskpool: кожа — список задач не получен", "err", err)
		return
	}
	var cands []*bankTask
	for i := range list {
		t := &list[i]
		if t.Meta == nil || t.Meta.Family == "" {
			continue
		}
		// Кожу с кожи — вечный 409: id кожи выводится из семьи и seed, поэтому
		// повторное переписывание даёт ровно тот же id.
		if strings.Contains(t.Meta.Generator, "+skin") {
			continue
		}
		cands = append(cands, t)
	}
	if len(cands) == 0 {
		// Кожа делается поверх задачи семьи: без них нечего переписывать.
		return
	}
	base := cands[j.cfg.Rand.Intn(len(cands))]

	skin, err := j.rewriteStatement(ctx, base, grade)
	if err != nil {
		j.log.Warn("taskpool: кожа — ответ модели не принят", "task", base.ID, "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"family":    base.Meta.Family,
		"seed":      base.Meta.Seed,
		"grade":     gradeOfID(base.ID),
		"statement": skin,
		"generator": j.cfg.LLM.Name(),
	})
	id, status, detail, err := j.postAdmin(ctx, "/admin/tasks/generate", body)
	if err != nil {
		j.log.Warn("taskpool: кожа не зарегистрирована", "task", base.ID, "status", status, "err", err, "detail", detail)
		return
	}
	if id == "" {
		if status == http.StatusConflict {
			j.log.Info("taskpool: кожа этой задачи уже в пуле", "task", base.ID)
		} else {
			j.log.Info("taskpool: кожа отбракована gate", "task", base.ID, "detail", detail)
		}
		return
	}
	j.log.Info("taskpool: кожа задачи в пуле", "id", id, "base", base.ID, "model", j.cfg.LLM.Name())
}

// rewriteStatement — промпт плюс защитные проверки ответа. Отказ обязателен:
// пустой или скопированный ответ породил бы дубликат, который просто занимает
// место в пуле.
func (j *Job) rewriteStatement(ctx context.Context, base *bankTask, grade string) (string, error) {
	domain := j.cfg.Domain
	if domain == "" {
		domain = "разработка"
	}
	// Порядок важен: сначала условие, потом инструкция. С инструкцией впереди
	// модель считала «формат оставь прежним» как «ничего не меняй» и возвращала
	// исходный текст дословно (проверено на живом vLLM).
	prompt := fmt.Sprintf(`Вот условие задачи (грейд: %s):

%s

Переделай его в задачу про область «%s». Правила:
- дословно повторяй только имена файлов и ключи/формат вывода — проверки задачи
  не меняются и сверяют вывод построчно;
- остальное перефразируй полностью: свой сюжет, свои примеры данных, свои
  формулировки;
- ничего не добавляй и не объясняй, выводи только текст условия.`, grade, base.Statement, domain)

	resp, err := j.cfg.LLM.Chat(ctx, llm.Request{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: 0.9,
		MaxTokens:   900,
	})
	if err != nil {
		return "", err
	}
	text := strings.TrimSpace(stripFences(resp.Content))
	switch {
	case len([]rune(text)) < 120:
		return "", fmt.Errorf("ответ модели слишком короткий (%d знаков)", len([]rune(text)))
	case normalizeSpace(text) == normalizeSpace(base.Statement):
		return "", fmt.Errorf("модель вернула исходное условие")
	}
	return text, nil
}

// fullGenTick — уровень C: модель пишет задачу целиком (условие, файлы, эталон,
// мутанты). Единственная причина, по которой это вообще безопасно, — pre-flight
// gate: эталон обязан пройти, мутанты обязаны упасть. Маленькая модель будет
// браковаться часто; это нормальный исход, а не поломка.
func (j *Job) fullGenTick(ctx context.Context) {
	stack := j.cfg.Stacks[j.cfg.Rand.Intn(len(j.cfg.Stacks))]
	grade := j.cfg.Grades[j.cfg.Rand.Intn(len(j.cfg.Grades))]

	d, err := j.askTaskJSON(ctx, stack, grade)
	if err != nil {
		j.log.Warn("taskpool: модель не сдала задачу в нужной форме", "stack", stack, "grade", grade, "err", err)
		return
	}
	body, _ := json.Marshal(map[string]any{
		"stack":     stack,
		"grades":    []string{grade},
		"title":     d.Title,
		"statement": d.Statement,
		"files":     d.Files,
		"reference": d.Reference,
		"mutants":   d.Mutants,
		"family":    "llm",
		"generator": j.cfg.LLM.Name(),
	})
	id, status, detail, err := j.postAdmin(ctx, "/admin/tasks", body)
	if err != nil {
		j.log.Warn("taskpool: регистрация модельной задачи не удалась", "status", status, "err", err, "detail", detail)
		return
	}
	if id == "" {
		j.log.Info("taskpool: модельная задача отбракована gate", "stack", stack, "grade", grade, "detail", detail)
		return
	}
	j.log.Info("taskpool: модельная задача в пуле", "id", id, "stack", stack, "grade", grade, "model", j.cfg.LLM.Name())
}

// draft — что модель обязана вернуть (строгий JSON, без пояснений).
type draft struct {
	Title     string              `json:"title"`
	Statement string              `json:"statement"`
	Files     map[string]string   `json:"files"`
	Reference map[string]string   `json:"reference"`
	Mutants   []map[string]string `json:"mutants"`
}

func (j *Job) askTaskJSON(ctx context.Context, stack, grade string) (*draft, error) {
	prompt := fmt.Sprintf(`Составь одну задачу для live-интервью, стек %s, уровень %s.

Формат ответа — строго JSON без пояснений:
{"title":"...","statement":"...","files":{"solution.sh":"...","check.sh":"...","input.txt":"...","empty.txt":""},"reference":{"solution.sh":"..."},"mutants":[{"solution.sh":"..."}]}

Обязательные правила:
- решение кандидата: solution.sh; данные для проверки: input.txt и empty.txt
  (файлы с такими именами берёт банк, подменить их нельзя);
- check.sh читает данные из своего каталога: DIR=$(cd -- "$(dirname -- "$0")" && pwd),
  затем запускает bash solution.sh "$DIR/input.txt";
- check.sh печатает OK и выходит 0 только на правильном решении, иначе exit 1;
- reference — решение, которое проходит check.sh;
- mutants — 3..5 решений с ОДНОЙ осмысленной ошибкой каждое (off-by-one, игнор
  регистра, неверное округление, забыт пустой ввод);
- в условии явно укажи формат вывода: строки, порядок, разделители.`, stack, grade)

	resp, err := j.cfg.LLM.Chat(ctx, llm.Request{
		Messages:    []llm.Message{{Role: "user", Content: prompt}},
		Temperature: 0.6,
		MaxTokens:   2500,
	})
	if err != nil {
		return nil, err
	}
	var d draft
	if err := json.Unmarshal([]byte(stripFences(resp.Content)), &d); err != nil {
		return nil, fmt.Errorf("ответ не JSON: %w", err)
	}
	if d.Statement == "" || len(d.Files) == 0 || len(d.Reference) == 0 || len(d.Mutants) == 0 {
		return nil, fmt.Errorf("в ответе нет statement/files/reference/mutants")
	}
	return &d, nil
}

// postAdmin — POST на админский маршрут sandbox. id == "" означает «не принято»
// (409 — такой seed уже в пуле, 422 — вердикт gate); detail — что ответили.
func (j *Job) postAdmin(ctx context.Context, path string, body []byte) (id string, status int, detail string, err error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, j.cfg.SandboxURL+path, strings.NewReader(string(body)))
	if err != nil {
		return "", 0, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+j.cfg.AdminToken)
	resp, err := j.cfg.HTTP.Do(req)
	if err != nil {
		return "", 0, "", err
	}
	defer resp.Body.Close()
	raw, _ := readLimited(resp.Body, 1<<20)
	detail = strings.TrimSpace(string(raw))
	switch resp.StatusCode {
	case http.StatusCreated:
		var out struct {
			ID string `json:"id"`
		}
		if err := json.Unmarshal(raw, &out); err != nil {
			return "", resp.StatusCode, detail, fmt.Errorf("разбор ответа: %w", err)
		}
		return out.ID, resp.StatusCode, detail, nil
	case http.StatusConflict, http.StatusUnprocessableEntity:
		return "", resp.StatusCode, detail, nil
	default:
		return "", resp.StatusCode, detail, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
}

// gradeOfID — грейд из id задачи вида family-grade-seed-hash (у кожи —
// family-grade-skin-seed-hash). Разбор с конца: seed и hash без '-'.
func gradeOfID(id string) string {
	parts := strings.Split(id, "-")
	if len(parts) < 4 {
		return ""
	}
	i := len(parts) - 3 // позиция перед парой seed-hash
	if parts[i] == "skin" && i-1 >= 0 {
		return parts[i-1]
	}
	return parts[i]
}

// stripFences — модели любят оборачивать ответ в ```json ... ```.
func stripFences(s string) string {
	s = strings.TrimSpace(s)
	if !strings.HasPrefix(s, "```") {
		return s
	}
	s = strings.TrimPrefix(s, "```")
	// Язык после ```, (json, markdown, ничего) — до первой строки.
	if i := strings.IndexByte(s, '\n'); i >= 0 && !strings.Contains(s[:i], "`") {
		s = s[i+1:]
	}
	if i := strings.LastIndex(s, "```"); i >= 0 {
		s = s[:i]
	}
	return strings.TrimSpace(s)
}

func normalizeSpace(s string) string {
	return strings.Join(strings.Fields(s), " ")
}
