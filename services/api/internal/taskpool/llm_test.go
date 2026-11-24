package taskpool

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

type fakeLLM struct {
	resp  string
	err   error
	calls int
	last  llm.Request
}

func (f *fakeLLM) Chat(_ context.Context, req llm.Request) (llm.Response, error) {
	f.calls++
	f.last = req
	if f.err != nil {
		return llm.Response{}, f.err
	}
	return llm.Response{Content: f.resp}, nil
}

func (f *fakeLLM) Name() string { return "fake" }

// skinAnswer — «переписанное» условие: тот же контракт вывода, другая обложка.
const skinAnswer = `Кладовщик выгружает остатки в файл: значения в строках, разделители — пробел, табуляция или запятая, встречается мусорная метка NA и пустые ячейки. ` +
	`Напишите solution.sh, который по пути к файлу печатает ровно 5 строк: sum=, n=, avg=, min=, max= в этом порядке. Пустой файл: sum=0, n=0, остальные NA.`

func newLLMJob(ts *httptest.Server, model LLM, skin, fullGen bool, f *fakeSandbox) *Job {
	cfg := Config{
		SandboxURL: ts.URL,
		AdminToken: "tok",
		Min:        1,
		MaxPerTick: 100,
		Interval:   time.Hour,
		HTTP:       nil,
		Rand:       rand.New(rand.NewSource(3)),
		LLM:        model,
		Skin:       skin,
		FullGen:    fullGen,
		Domain:     "складской учёт",
	}
	j := New(cfg, slog.New(slog.DiscardHandler))
	j.cfg.HTTP = ts.Client()
	return j
}

// TestSkinDisabled — без флага модель не дёргается: включённый LLM-проход —
// решение оператора, а не побочный эффект наличия модели.
func TestSkinDisabled(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	m := &fakeLLM{resp: skinAnswer}
	if made := newLLMJob(ts, m, false, false, f).pass(context.Background()); made != 0 {
		t.Fatalf("с выполненным порогом принято %d задач", made)
	}
	if m.calls != 0 {
		t.Fatalf("модель вызвана %d раз при выключенных уровнях B/C", m.calls)
	}
}

// TestSkinRegisters — кожа регистрируется отдельной задачей: та же семья и seed,
// новое условие, в запросе видно модель.
func TestSkinRegisters(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	m := &fakeLLM{resp: skinAnswer}
	newLLMJob(ts, m, true, false, f).pass(context.Background())

	if m.calls != 1 {
		t.Fatalf("обращений к модели %d, ожидалось 1 (одна попытка за тик)", m.calls)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 1 {
		t.Fatalf("запросов генерации %d, ожидалось 1: %+v", len(f.attempts), f.attempts)
	}
	a := f.attempts[0]
	if a.Statement != skinAnswer {
		t.Fatalf("условие кожи передано неверно: %.60s", a.Statement)
	}
	if a.Family != "shell-stats" || a.Seed <= 0 {
		t.Fatalf("кожа должна просить ту же семью и seed: %+v", a)
	}
	if a.Generator != "fake" {
		t.Fatalf("в generator нет модели: %q", a.Generator)
	}
}

// TestSkinRejectsCopy — модель вернула исходное условие: заявлять в пул
// дубликат незачем.
func TestSkinRejectsCopy(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	newLLMJob(ts, &fakeLLM{resp: baseStatement}, true, false, f).pass(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 0 {
		t.Fatalf("копия условия ушла в пул: %+v", f.attempts)
	}
}

// TestSkinRejectsShortAnswer — ответ «ок» или обрезок не условие задачи.
func TestSkinRejectsShortAnswer(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	newLLMJob(ts, &fakeLLM{resp: "Посчитай сумму и количество."}, true, false, f).pass(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 0 {
		t.Fatalf("короткий ответ ушёл в пул: %+v", f.attempts)
	}
}

// TestSkinStripsFences — модель часто оборачивает ответ в ```...```.
func TestSkinStripsFences(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	newLLMJob(ts, &fakeLLM{resp: "```markdown\n" + skinAnswer + "\n```"}, true, false, f).pass(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 1 || f.attempts[0].Statement != skinAnswer {
		t.Fatalf("markdown-ограждение не снято: %+v", f.attempts)
	}
}

// TestFullGenSendsDraft — уровень C: содержимое, которое дала модель, уходит на
// регистрацию целиком (решает gate, а не мы).
func TestFullGenSendsDraft(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	f.registerStatus = 201
	ans, _ := json.Marshal(map[string]any{
		"title":     "Посчитать строки",
		"statement": "Напечатать число непустых строк файла.",
		"files":     map[string]string{"solution.sh": "#!/usr/bin/env bash\n", "check.sh": "#!/usr/bin/env bash\necho OK\n", "input.txt": "a\n\nb\n", "empty.txt": ""},
		"reference": map[string]string{"solution.sh": "#!/usr/bin/env bash\ngrep -c . \"$1\"\n"},
		"mutants":   []map[string]string{{"solution.sh": "#!/usr/bin/env bash\nwc -l < \"$1\"\n"}},
	})
	m := &fakeLLM{resp: "```json\n" + string(ans) + "\n```"}
	newLLMJob(ts, m, false, true, f).pass(context.Background())

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.registered) != 1 {
		t.Fatalf("регистраций %d, ожидалось 1", len(f.registered))
	}
	rec := f.registered[0]
	if _, ok := rec["files"].(map[string]any)["check.sh"]; !ok {
		t.Fatalf("файлы задачи не переданы: %v", rec["files"])
	}
	if rec["generator"] != "fake" || rec["family"] != "llm" {
		t.Fatalf("meta генерации потеряна: %v / %v", rec["generator"], rec["family"])
	}
}

// TestFullGenGateRejectIsNormal — 422 от gate не авария: одна попытка за тик,
// повтор на следующем.
func TestFullGenGateRejectIsNormal(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	f.registerStatus = 422
	ans, _ := json.Marshal(map[string]any{
		"title":     "Посчитать строки",
		"statement": "Напечатать число непустых строк файла данных, который передаётся первым аргументом скрипту.",
		"files":     map[string]string{"solution.sh": "#!/usr/bin/env bash\n", "check.sh": "#!/usr/bin/env bash\necho OK\n"},
		"reference": map[string]string{"solution.sh": "#!/usr/bin/env bash\necho 1\n"},
		"mutants":   []map[string]string{{"solution.sh": "#!/usr/bin/env bash\necho 2\n"}},
	})
	newLLMJob(ts, &fakeLLM{resp: string(ans)}, false, true, f).pass(context.Background())

	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.registered) != 1 {
		t.Fatalf("после брака gate попыток %d, ожидалось 1", len(f.registered))
	}
}

// TestFullGenRejectsNonJSON — модель ответила прозой: задача никуда не идёт.
func TestFullGenRejectsNonJSON(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	newLLMJob(ts, &fakeLLM{resp: "Вот задача: посчитайте строки в файле."}, false, true, f).pass(context.Background())
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.registered) != 0 {
		t.Fatalf("неструктурированный ответ ушёл на регистрацию")
	}
}

// TestGradeOfID — разбор id: кожа отличается от базовой задачи вставкой -skin-,
// и грейд нужно доставать из обеих форм.
func TestGradeOfID(t *testing.T) {
	cases := map[string]string{
		"shell-stats-junior-7-abcd12":       "junior",
		"shell-stats-staff-175533-e18a46":   "staff",
		"shell-stats-middle-skin-21-6389b4": "middle",
		"unexpected":                        "",
	}
	for id, want := range cases {
		if got := gradeOfID(id); got != want {
			t.Errorf("gradeOfID(%q) = %q, ожидалось %q", id, got, want)
		}
	}
}

// TestSkinSkipsSkinnedTask — кожа делается только поверх базовой задачи семьи.
// Кожа с кожи дала бы тот же id (он выводится из семьи и seed) и уткнулась бы в
// 409 на каждом тике.
func TestSkinSkipsSkinnedTask(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 1
	}
	// Единственная задача в пуле — уже кожа.
	f.listOverride = func(grade string) []map[string]any {
		return []map[string]any{{
			"id":        "shell-stats-" + grade + "-skin-9-abcd12",
			"statement": baseStatement,
			"meta":      map[string]any{"family": "shell-stats", "seed": 9, "generator": "family:shell-stats+skin:qwen"},
		}}
	}
	m := &fakeLLM{resp: skinAnswer}
	newLLMJob(ts, m, true, false, f).pass(context.Background())
	if m.calls != 0 {
		t.Fatalf("модель дёргалась %d раз ради уже переписанной задачи", m.calls)
	}
}

// TestSkinSkipsAlreadySkinned — у базовой задачи уже есть кожа: кандидата не
// отдаём модели (зачем платить секундой LLM за заведомый 409).
func TestSkinSkipsAlreadySkinned(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 2
	}
	f.listOverride = func(grade string) []map[string]any {
		return []map[string]any{
			{"id": "shell-stats-" + grade + "-9-abcd12", "statement": baseStatement,
				"meta": map[string]any{"family": "shell-stats", "seed": 9, "generator": "family:shell-stats"}},
			{"id": "shell-stats-" + grade + "-skin-9-abcd12", "statement": skinAnswer,
				"meta": map[string]any{"family": "shell-stats", "seed": 9, "generator": "family:shell-stats+skin:qwen"}},
		}
	}
	m := &fakeLLM{resp: skinAnswer}
	newLLMJob(ts, m, true, false, f).pass(context.Background())
	if m.calls != 0 {
		t.Fatalf("модель вызвана %d раз, хотя кожа этой задачи уже в пуле", m.calls)
	}
}
