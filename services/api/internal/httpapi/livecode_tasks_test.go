package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"math/rand"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// liveCodeBankTask — задача банка для mock-sandbox.
func liveCodeBankTask(id string) map[string]any {
	return map[string]any{
		"id":        id,
		"stack":     "go",
		"grades":    []string{"middle"},
		"title":     "Задача " + id,
		"statement": "Условие " + id,
		"files":     map[string]string{"solution.go": "package task\n"},
	}
}

// sandboxTasksHandler — mock sandbox GET /api/v1/tasks?stack=&grade=.
func sandboxTasksHandler(pools map[string][]map[string]any) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.URL.Query().Get("stack") + "|" + r.URL.Query().Get("grade")
		w.Header().Set("Content-Type", "application/json")
		out := []map[string]any{}
		for _, t := range pools[key] {
			out = append(out, t)
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func idsOf(list []map[string]any) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, fmt.Sprint(t["id"]))
	}
	return out
}

func TestPickLiveCodeTasks(t *testing.T) {
	pool := make([]liveCodeTask, 0, 5)
	for i := 0; i < 5; i++ {
		pool = append(pool, liveCodeTask{ID: fmt.Sprintf("m%d", i), Title: fmt.Sprintf("m%d", i)})
	}
	stretch := []liveCodeTask{{ID: "s0", Title: "s0"}, {ID: "s1", Title: "s1"}}

	// middle: N=3, последняя — stretch, без повторов.
	for _, seed := range []int64{1, 2, 42} {
		rng := rand.New(rand.NewSource(seed))
		got, _ := pickLiveCodeTasks(models.GradeMiddle, pool, stretch, nil, rng)
		if len(got) != 3 {
			t.Fatalf("middle: len = %d, want 3", len(got))
		}
		seen := map[string]bool{}
		for i, task := range got {
			if seen[task.ID] {
				t.Fatalf("повтор: %v", idsOf2(got))
			}
			seen[task.ID] = true
			if i < 2 && task.ID[:1] != "m" {
				t.Fatalf("позиция %d не из основного пула: %s", i, task.ID)
			}
		}
		if got[2].ID != "s0" && got[2].ID != "s1" {
			t.Fatalf("последняя не из stretch-пула: %v", idsOf2(got))
		}
	}

	// junior: N=2, без stretch — только основной пул.
	rng := rand.New(rand.NewSource(1))
	got, _ := pickLiveCodeTasks(models.GradeJunior, pool, nil, nil, rng)
	if len(got) != 2 {
		t.Fatalf("junior: len = %d, want 2", len(got))
	}
	if got[0].ID == got[1].ID {
		t.Fatalf("junior: повторы")
	}

	// Пул меньше N — взять всё (деградация).
	small := pool[:1]
	got, _ = pickLiveCodeTasks(models.GradeMiddle, small, nil, nil, rand.New(rand.NewSource(1)))
	if len(got) != 1 || got[0].ID != "m0" {
		t.Fatalf("малый пул: %v", idsOf2(got))
	}

	// staff: без stretch.
	got, _ = pickLiveCodeTasks(models.GradeStaff, pool, nil, nil, rand.New(rand.NewSource(1)))
	if len(got) != 3 {
		t.Fatalf("staff: len = %d, want 3", len(got))
	}
}

func idsOf2(list []liveCodeTask) []string {
	out := make([]string, 0, len(list))
	for _, t := range list {
		out = append(out, t.ID)
	}
	return out
}

// TestEnterLiveCodeSelection — WS-интеграция: N задач, task=tasks[0], stretch
// (последняя из senior-пула для middle), детерминизм по LIVECODE_TASK_SEED,
// событие livecode_tasks.
func TestEnterLiveCodeSelection(t *testing.T) {
	t.Setenv("LIVECODE_TASK_SEED", "1")
	middlePool := []map[string]any{
		liveCodeBankTask("go-m1"), liveCodeBankTask("go-m2"), liveCodeBankTask("go-m3"),
		liveCodeBankTask("go-m4"), liveCodeBankTask("go-m5"),
	}
	seniorPool := []map[string]any{liveCodeBankTask("go-s1"), liveCodeBankTask("go-s2")}
	e := newRunsEnv(t, sandboxTasksHandler(map[string][]map[string]any{
		"go|middle": middlePool,
		"go|senior": seniorPool,
	}))

	enter := func() []string {
		t.Helper()
		conn := dialWS(t, e.ts, e.token, e.session)
		defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
		// Стартовые: stage/voice + ai_text.
		_ = wsReadTypes(t, conn, 2, 3*time.Second)
		wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
			"payload": map[string]string{"stage": "livecode"}})
		// stage (движок) → stage (enterLiveCode, с tasks) → ai_text (mock).
		raw := wsReadRaw(t, conn, 3, 5*time.Second)
		var stageMsg map[string]any
		for _, b := range raw {
			var m map[string]any
			if err := json.Unmarshal(b, &m); err != nil {
				continue
			}
			if m["type"] == "stage" && m["tasks"] != nil {
				stageMsg = m
			}
		}
		if stageMsg == nil {
			t.Fatalf("нет stage-сообщения с tasks: %s", string(raw[1]))
		}
		tasks, _ := stageMsg["tasks"].([]any)
		if len(tasks) != 3 {
			t.Fatalf("tasks: %d, want 3 (middle)", len(tasks))
		}
		// task = tasks[0] (совместимость).
		task, _ := stageMsg["task"].(map[string]any)
		first, _ := tasks[0].(map[string]any)
		if task == nil || task["id"] != first["id"] {
			t.Fatalf("task != tasks[0]: %v vs %v", task, first)
		}
		ids := make([]string, 0, 3)
		for i, item := range tasks {
			tm, _ := item.(map[string]any)
			id := fmt.Sprint(tm["id"])
			ids = append(ids, id)
			if i < 2 && !(id >= "go-m1" && id <= "go-m5") {
				t.Fatalf("позиция %d не из middle-пула: %s", i, id)
			}
		}
		// Stretch: последняя — из senior-пула (spec-13B T1.2).
		if ids[2] != "go-s1" && ids[2] != "go-s2" {
			t.Fatalf("последняя не из senior-пула: %v", ids)
		}
		return ids
	}

	first := enter()
	// Событие livecode_tasks сохранено (для отчёта).
	events, err := e.server.sessions.ListEvents(context.Background(), e.session)
	if err != nil {
		t.Fatalf("events: %v", err)
	}
	found := false
	for _, ev := range events {
		if ev.Kind == "livecode_tasks" {
			found = true
		}
	}
	if !found {
		t.Fatalf("нет события livecode_tasks: %+v", events)
	}

	// Детерминизм: та же seed у кандидата без истории → тот же выбор.
	code, reg := doJSON(t, "POST", e.ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "fresh-candidate@example.ru", "password": "password1"}, nil)
	if code != http.StatusCreated {
		t.Fatalf("register: %d %v", code, reg)
	}
	freshToken, _ := reg["token"].(string)
	freshSession, _ := createSession(t, e.ts, freshToken, "middle", "go")
	e.session, e.token = freshSession, freshToken
	same := enter()
	for i := range first {
		if first[i] != same[i] {
			t.Fatalf("seed=1: кандидат без истории получил другой выбор: %v vs %v", first, same)
		}
	}

	// Новая сессия того же кандидата — другие задачи (spec-24: одну и ту же задачу
	// кандидат не решает дважды; банк позволяет выбрать свежие).
	e2session, _ := createSession(t, e.ts, e.token, "middle", "go")
	e.session = e2session
	second := enter()
	sameAgain := true
	for i := range first {
		if first[i] != second[i] {
			sameAgain = false
			break
		}
	}
	if sameAgain {
		t.Fatalf("seed=1: вторая сессия того же кандидата получила те же задачи: %v", second)
	}
}

// bankTask — задача банка sandbox с явными стеком, грейдом и файлами.
func bankTask(id, stack, grade string, files map[string]string) map[string]any {
	return map[string]any{
		"id": id, "stack": stack, "grades": []string{grade},
		"title": "Задача " + id, "statement": "Условие " + id, "files": files,
	}
}

// bankProbe — какие stack были запрошены у mock-банка.
type bankProbe struct {
	mu     sync.Mutex
	stacks []string
}

func (p *bankProbe) add(stack string) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.stacks = append(p.stacks, stack)
}

func (p *bankProbe) all() []string {
	p.mu.Lock()
	defer p.mu.Unlock()
	return append([]string(nil), p.stacks...)
}

// sandboxBankHandler — mock sandbox GET /api/v1/tasks с семантикой Bank.List
// (services/sandbox/internal/tasks/tasks.go): пустой stack снимает фильтр по
// стеку, то есть отдаёт задачи всех стеков.
func sandboxBankHandler(bank []map[string]any, probe *bankProbe) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		stack, grade := r.URL.Query().Get("stack"), r.URL.Query().Get("grade")
		probe.add(stack)
		out := []map[string]any{}
		for _, t := range bank {
			if stack != "" && t["stack"] != stack {
				continue
			}
			for _, g := range t["grades"].([]string) {
				if g == grade {
					out = append(out, t)
					break
				}
			}
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(out)
	}
}

// TestSanitizeTasksGraderFiles — issue #3: файлы градатора вырезаются по всем
// маскам рантаймов сразу: стек стадии может не совпасть со стеком задачи.
func TestSanitizeTasksGraderFiles(t *testing.T) {
	got := sanitizeTasks([]liveCodeTask{{ID: "x", Files: map[string]string{
		"solution.go": "package main\n", "solution_test.go": "package main\n",
		"solution.py": "x = 1\n", "test_solution.py": "def t():\n",
		"solution_test.py": "def t():\n", "solution.sh": "#!/bin/sh\n",
		"check.sh": "#!/bin/sh\n", "access.log": "1.2.3.4\n",
	}}})
	want := []string{"access.log", "solution.go", "solution.py", "solution.sh"}
	if len(got[0].Files) != len(want) {
		t.Fatalf("файлы: %v, want %v", got[0].Files, want)
	}
	for _, name := range want {
		if _, ok := got[0].Files[name]; !ok {
			t.Fatalf("нет %s: %v", name, got[0].Files)
		}
	}
}

// TestEnterLiveCodeEmptySessionStack — issue #3: legacy-сессия (пустой stack в БД,
// роль sysadmin с практикой shell). Оба пула берутся по стеку роли; stretch не
// уходит в банк без фильтра (иначе senior-задачи go/python с открытыми тестами
// градатора приходят кандидату).
func TestEnterLiveCodeEmptySessionStack(t *testing.T) {
	t.Setenv("LIVECODE_TASK_SEED", "1")
	bank := []map[string]any{
		bankTask("shell-m1", "shell", "middle", map[string]string{"solution.sh": "#!/bin/sh\n"}),
		bankTask("shell-m2", "shell", "middle", map[string]string{"solution.sh": "#!/bin/sh\n"}),
		bankTask("shell-m3", "shell", "middle", map[string]string{"solution.sh": "#!/bin/sh\n"}),
		bankTask("shell-s1", "shell", "senior", map[string]string{"solution.sh": "#!/bin/sh\n"}),
		// senior-чужих стеков: попадают в выдачу только при запросе stack=''.
		bankTask("go-s1", "go", "senior", map[string]string{
			"solution.go": "package main\n", "solution_test.go": "package main\n",
		}),
		bankTask("py-s1", "python", "senior", map[string]string{
			"solution.py": "x = 1\n", "test_solution.py": "def test_x():\n",
		}),
	}
	probe := &bankProbe{}
	e := newRunsEnv(t, sandboxBankHandler(bank, probe))

	ctx := context.Background()
	u, err := e.server.users.GetUserByEmail(ctx, "runs@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	legacy, err := e.server.sessions.Create(ctx, models.Session{
		UserID: u.ID, Grade: models.GradeMiddle, Stack: "", Stage: models.StageVoice,
		Status: models.StatusActive, DurationLimitS: 2700, StartedAt: time.Now(),
		TrackID: "sysadmin",
	})
	if err != nil {
		t.Fatalf("legacy session: %v", err)
	}
	e.session = legacy.ID

	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	_ = wsReadTypes(t, conn, 2, 3*time.Second)
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
		"payload": map[string]string{"stage": "livecode"}})
	raw := wsReadRaw(t, conn, 3, 5*time.Second)

	var tasks []any
	for _, b := range raw {
		var m map[string]any
		if err := json.Unmarshal(b, &m); err == nil && m["type"] == "stage" && m["tasks"] != nil {
			tasks, _ = m["tasks"].([]any)
		}
	}
	if len(tasks) == 0 {
		t.Fatalf("нет stage-сообщения с tasks: %s", raw[0])
	}
	for _, item := range tasks {
		tm, _ := item.(map[string]any)
		id := fmt.Sprint(tm["id"])
		if !strings.HasPrefix(id, "shell-") {
			t.Fatalf("задача чужого стека (stack='' ушла в банк): %v", idsFromMsg(tasks))
		}
		files, _ := tm["files"].(map[string]any)
		for name := range files {
			if strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.py") ||
				strings.HasPrefix(name, "test_") || name == "check.sh" {
				t.Fatalf("файл градатора ушёл кандидату (%s): %s", id, name)
			}
		}
	}
	// stretch middle→senior жив: последняя задача — из shell senior-пула.
	last := tasks[len(tasks)-1].(map[string]any)
	if got := fmt.Sprint(last["id"]); got != "shell-s1" {
		t.Fatalf("stretch: %s, want shell-s1 (все: %v)", got, idsFromMsg(tasks))
	}
	// В банк не уходило ни одного запроса без фильтра по стеку.
	for _, stack := range probe.all() {
		if stack == "" {
			t.Fatalf("запрос к банку без стека: %v", probe.all())
		}
	}
}

// TestEnterLiveCodeUnknownRole — Stack сессии пуст и роль не найдена (роль
// убрали из файла / подменили ROLE_TRACKS_FILE): задачи не берутся вообще,
// запроса в банк без стека нет.
func TestEnterLiveCodeUnknownRole(t *testing.T) {
	probe := &bankProbe{}
	e := newRunsEnv(t, sandboxBankHandler([]map[string]any{
		bankTask("go-m1", "go", "middle", map[string]string{"solution.go": "package main\n"}),
	}, probe))

	ctx := context.Background()
	u, err := e.server.users.GetUserByEmail(ctx, "runs@example.com")
	if err != nil {
		t.Fatalf("user: %v", err)
	}
	sess, err := e.server.sessions.Create(ctx, models.Session{
		UserID: u.ID, Grade: models.GradeMiddle, Stack: "", Stage: models.StageVoice,
		Status: models.StatusActive, DurationLimitS: 2700, StartedAt: time.Now(),
		TrackID: "no-such-role",
	})
	if err != nil {
		t.Fatalf("session: %v", err)
	}
	e.session = sess.ID

	conn := dialWS(t, e.ts, e.token, e.session)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()
	_ = wsReadTypes(t, conn, 2, 3*time.Second)
	wsWriteJSON(t, conn, map[string]any{"type": "ui", "name": "stage_action",
		"payload": map[string]string{"stage": "livecode"}})
	// Сообщений меньше трёх (stage с задачами нет) — читаем до таймаута.
	ctxRead, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	for {
		_, b, err := conn.Read(ctxRead)
		if err != nil {
			break
		}
		var m map[string]any
		if err := json.Unmarshal(b, &m); err == nil && m["type"] == "stage" && m["tasks"] != nil {
			t.Fatalf("задачи отданы при неизвестной роли: %v", m["tasks"])
		}
	}
	for _, stack := range probe.all() {
		if stack == "" {
			t.Fatalf("запрос к банку без стека: %v", probe.all())
		}
	}
}

func idsFromMsg(list []any) []string {
	out := make([]string, 0, len(list))
	for _, item := range list {
		tm, _ := item.(map[string]any)
		out = append(out, fmt.Sprint(tm["id"]))
	}
	return out
}
