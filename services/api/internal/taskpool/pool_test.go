package taskpool

import (
	"context"
	"encoding/json"
	"log/slog"
	"math/rand"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"testing"
	"time"
)

type fakeSandbox struct {
	mu             sync.Mutex
	counts         map[string]int // "stack|grade" -> число задач
	gateOK         bool
	authWant       string
	attempts       []genAttempt
	registered     []map[string]any
	registerStatus int
	families       []Family
	listOverride   func(grade string) []map[string]any // подмена выдачи /api/v1/tasks
	unauthOK       bool                                // принимать запросы без токена (проверка, что токен реально шлётся)
}

type genAttempt struct {
	Family    string `json:"family"`
	Seed      int64  `json:"seed"`
	Grade     string `json:"grade"`
	Statement string `json:"statement"`
	Generator string `json:"generator"`
	Auth      string
}

// baseStatement — условие семейной задачи (с ним кожа сравнивает ответ модели).
const baseStatement = `Напишите bash-скрипт solution.sh: первый аргумент — путь к файлу данных. ` +
	`Формат данных: строки, значения разделены пробелом или запятой. Мусор NA не учитывается. ` +
	`Напечатать ровно 5 строк: sum=, n=, avg=, min=, max=. Проверка сверяет вывод построчно.`

func newFake(t *testing.T) (*fakeSandbox, *httptest.Server) {
	t.Helper()
	f := &fakeSandbox{
		counts:   map[string]int{},
		gateOK:   true,
		authWant: "Bearer tok",
		families: []Family{{ID: "shell-stats", Stacks: []string{"shell"}, Grades: []string{"junior", "middle", "senior", "staff"}}},
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/v1/tasks", func(w http.ResponseWriter, r *http.Request) {
		q := r.URL.Query()
		f.mu.Lock()
		n := f.counts[q.Get("stack")+"|"+q.Get("grade")]
		f.mu.Unlock()
		grade := q.Get("grade")
		if f.listOverride != nil {
			_ = json.NewEncoder(w).Encode(f.listOverride(grade))
			return
		}
		list := make([]map[string]any, n)
		for i := range list {
			// id и meta — как у настоящей семейной задачи: кожа разбирает их,
			// чтобы попросить пересборку той же семьи с тем же seed.
			list[i] = map[string]any{
				"id":        "shell-stats-" + grade + "-" + strconv.Itoa(i+1) + "-abcd12",
				"statement": baseStatement,
				"meta":      map[string]any{"family": "shell-stats", "seed": i + 1, "generator": "family:shell-stats"},
			}
		}
		_ = json.NewEncoder(w).Encode(list)
	})
	mux.HandleFunc("GET /admin/families", func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != f.authWant {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"families": f.families})
	})
	mux.HandleFunc("POST /admin/tasks/generate", func(w http.ResponseWriter, r *http.Request) {
		var a genAttempt
		if err := json.NewDecoder(r.Body).Decode(&a); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		a.Auth = r.Header.Get("Authorization")
		f.mu.Lock()
		f.attempts = append(f.attempts, a)
		f.mu.Unlock()
		if !f.unauthOK && a.Auth != f.authWant {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if !f.gateOK {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"ok":false,"reasons":["mutant-1 прошёл тесты"]}`))
			return
		}
		f.mu.Lock()
		key := "shell|" + a.Grade
		f.counts[key]++
		f.mu.Unlock()
		id := a.Family + "-" + a.Grade
		if a.Statement != "" {
			id += "-skin"
		}
		w.WriteHeader(http.StatusCreated)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": id})
	})
	// Уровень C: готовое содержимое задачи; исход ответа настраивает тест.
	mux.HandleFunc("POST /admin/tasks", func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		f.mu.Lock()
		f.registered = append(f.registered, body)
		status := f.registerStatus
		f.mu.Unlock()
		if status != http.StatusCreated {
			w.WriteHeader(http.StatusUnprocessableEntity)
			_, _ = w.Write([]byte(`{"ok":false,"reasons":["mutant-2 прошёл тесты"]}`))
			return
		}
		w.WriteHeader(status)
		_ = json.NewEncoder(w).Encode(map[string]string{"id": "llm-task-1"})
	})
	ts := httptest.NewServer(mux)
	t.Cleanup(ts.Close)
	return f, ts
}

func newJob(ts *httptest.Server, min, perTick int) *Job {
	return New(Config{
		SandboxURL: ts.URL,
		AdminToken: "tok",
		Min:        min,
		MaxPerTick: perTick,
		Interval:   time.Hour,
		HTTP:       ts.Client(),
		Rand:       rand.New(rand.NewSource(7)),
	}, slog.New(slog.DiscardHandler))
}

// TestNewDisabled — без токена/адреса задача не создаётся: выключенная функция
// не должна светиться ошибками в логах каждый тик.
func TestNewDisabled(t *testing.T) {
	if New(Config{SandboxURL: "http://x"}, slog.New(slog.DiscardHandler)) != nil {
		t.Fatalf("без SANDBOX_ADMIN_TOKEN задача должна быть выключена")
	}
	if New(Config{AdminToken: "t"}, slog.New(slog.DiscardHandler)) != nil {
		t.Fatalf("без SANDBOX_URL задача должна быть выключена")
	}
}

// TestPassFillsToMin — пул доводится до порога по каждомугрейду, с токеном.
func TestPassFillsToMin(t *testing.T) {
	f, ts := newFake(t)
	f.counts["shell|junior"] = 1
	job := newJob(ts, 3, 100)
	made := job.pass(context.Background())
	if made != 11 { // junior: 3-1, остальные по 3
		t.Fatalf("принято %d задач, ожидалось 11", made)
	}
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		if got := f.counts["shell|"+g]; got != 3 {
			t.Errorf("%s: в пуле %d задач, ожидалось 3", g, got)
		}
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	for _, a := range f.attempts {
		if a.Auth != "Bearer tok" {
			t.Fatalf("запрос на генерацию без токена: %q", a.Auth)
		}
		if a.Family != "shell-stats" || a.Seed <= 0 {
			t.Fatalf("некорректный запрос генерации: %+v", a)
		}
	}
}

// TestPassAboveMinDoesNothing — порог выполнен — генерации нет (не «генерируем
// всегда, пока жив процесс»).
func TestPassAboveMinDoesNothing(t *testing.T) {
	f, ts := newFake(t)
	for _, g := range []string{"junior", "middle", "senior", "staff"} {
		f.counts["shell|"+g] = 4
	}
	if made := newJob(ts, 4, 10).pass(context.Background()); made != 0 {
		t.Fatalf("сгенерировано %d задач при выполненном пороге", made)
	}
}

// TestMaxPerTick — потолок за проход: сломанный генератор не гоняет gate
// бесконечно.
func TestMaxPerTick(t *testing.T) {
	f, ts := newFake(t)
	if made := newJob(ts, 5, 2).pass(context.Background()); made != 2 {
		t.Fatalf("принято %d, ожидалось 2 (MaxPerTick)", made)
	}
	total := 0
	for _, n := range f.counts {
		total += n
	}
	if total != 2 {
		t.Fatalf("в пуле %d задач, ожидалось 2", total)
	}
}

// TestGateRejectStops — gate бракует всё: один прогон на пару (стек, грейд) и
// выход, а не внутренний цикл до бесконечности.
func TestGateRejectStops(t *testing.T) {
	f, ts := newFake(t)
	f.gateOK = false
	made := newJob(ts, 3, 100).pass(context.Background())
	if made != 0 {
		t.Fatalf("принято %d задач, хотя gate всё отверг", made)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 4 { // по одной попытке на грейд
		t.Fatalf("попыток генерации %d, ожидалось 4", len(f.attempts))
	}
}

// TestFamilyMismatch — для стека нет семьи: генерации нет (иначе слались бы
// запросы заведомо в ошибку).
func TestFamilyMismatch(t *testing.T) {
	f, ts := newFake(t)
	f.families = []Family{{ID: "go-algo", Stacks: []string{"go"}, Grades: []string{"senior"}}}
	if made := newJob(ts, 3, 100).pass(context.Background()); made != 0 {
		t.Fatalf("сгенерировано %d задач без подходящей семьи", made)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if len(f.attempts) != 0 {
		t.Fatalf("поступили запросы генерации без подходящей семьи: %+v", f.attempts)
	}
}

// TestRunStopsOnContext — цикл уважает отмену (иначе goroutine утекает при
// shutdown).
func TestRunStopsOnContext(t *testing.T) {
	_, ts := newFake(t)
	job := newJob(ts, 1, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() {
		job.Run(ctx)
		close(done)
	}()
	cancel()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatalf("Run не завершился по отмене ctx")
	}
}
