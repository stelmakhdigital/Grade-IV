package main

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/taskgen"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

func postJSON(t *testing.T, url, token string, body any) (*http.Response, map[string]any) {
	t.Helper()
	raw, err := json.Marshal(body)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	req, _ := http.NewRequest("POST", url, bytes.NewReader(raw))
	if token != "" {
		req.Header.Set("Authorization", "Bearer "+token)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("post %s: %v", url, err)
	}
	defer resp.Body.Close()
	var out map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("разбор ответа %s: %v", url, err)
	}
	return resp, out
}

// TestAdminGenerateDisabled — без токена генерация закрыта (503), а не «пустой
// токен = доступ для всех».
func TestAdminGenerateDisabled(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	bank, err := tasks.Load("")
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	ts := httptest.NewServer(buildMux("subprocess", bank, runner.New(runner.Config{Mode: "subprocess"}), "", log))
	defer ts.Close()

	resp, body := postJSON(t, ts.URL+"/admin/tasks/generate", "", map[string]any{"family": "shell-stats", "seed": 1, "grade": "junior"})
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("без SANDBOX_ADMIN_TOKEN ожидалось 503, получено %d (%v)", resp.StatusCode, body)
	}
}

// TestAdminGenerateUnauthorized — токен задан, но запрос без него/с чужим.
func TestAdminGenerateUnauthorized(t *testing.T) {
	ts, _ := newTestServerWithBank(t, "subprocess", filepath.Join(t.TempDir(), "gen.json"))
	resp, _ := postJSON(t, ts.URL+"/admin/tasks/generate", "", map[string]any{"family": "shell-stats", "seed": 1, "grade": "junior"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ожидалось 401, получено %d", resp.StatusCode)
	}
	resp, _ = postJSON(t, ts.URL+"/admin/tasks/generate", "wrong", map[string]any{"family": "shell-stats", "seed": 1, "grade": "junior"})
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("с чужим токеном ожидалось 401, получено %d", resp.StatusCode)
	}
}

// TestAdminGenerateAndUse — полный путь: семья → gate → банк → задача видна в
// /api/v1/tasks и сдаётся через /runs (то есть overlay живой, а не «записали и
// забыли»).
func TestAdminGenerateAndUse(t *testing.T) {
	if testing.Short() {
		t.Skip("реальные прогоны bash в песочнице (не -short)")
	}
	file := filepath.Join(t.TempDir(), "gen.json")
	ts, token := newTestServerWithBank(t, "subprocess", file)

	resp, body := postJSON(t, ts.URL+"/admin/tasks/generate", token, map[string]any{"family": "shell-stats", "seed": 11, "grade": "junior"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ожидалось 201, получено %d: %v", resp.StatusCode, body)
	}
	id, _ := body["id"].(string)
	if id == "" {
		t.Fatalf("в ответе нет id: %v", body)
	}

	var listed []struct {
		ID   string `json:"id"`
		Meta *struct {
			Family string `json:"family"`
			Hash   string `json:"hash"`
		} `json:"meta"`
	}
	raw, err := http.Get(ts.URL + "/api/v1/tasks?stack=shell&grade=junior")
	if err != nil {
		t.Fatalf("tasks list: %v", err)
	}
	defer raw.Body.Close()
	if err := json.NewDecoder(raw.Body).Decode(&listed); err != nil {
		t.Fatalf("разбор списка задач: %v", err)
	}
	found := false
	for _, tl := range listed {
		if tl.ID == id {
			found = true
			if tl.Meta == nil || tl.Meta.Family != "shell-stats" || tl.Meta.Hash == "" {
				t.Fatalf("у сгенерированной задачи нет meta: %+v", tl)
			}
		}
	}
	if !found {
		t.Fatalf("задача %s не появилась в GET /api/v1/tasks", id)
	}

	// Эталон прогоняется уже как обычная задача банка — значит overlay читается
	// тем же Putем, что и встроенные задачи.
	draft, err := taskgen.Build("shell-stats", 11, "junior")
	if err != nil {
		t.Fatalf("build: %v", err)
	}
	// Сдаём только решение: данные чек берёт из банка (IsTrustedFile), их в
	// сабмите быть не должно.
	runBody := map[string]any{"stack": "shell", "action": "test", "task_id": id, "files": draft.Reference}
	resp, run := postJSON(t, fmt.Sprintf("%s/api/v1/sessions/1/runs", ts.URL), "", runBody)
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("run: %d %v", resp.StatusCode, run)
	}
	if run["passed"] != true {
		t.Fatalf("эталон не прошёл сгенерированную задачу: %v", run)
	}
}

// TestAdminGeneratePersists — рестарт сервиса (новый Load того же файла) не
// теряет принятые задачи.
func TestAdminGeneratePersists(t *testing.T) {
	if testing.Short() {
		t.Skip("реальные прогоны bash в песочнице (не -short)")
	}
	file := filepath.Join(t.TempDir(), "gen.json")
	ts, token := newTestServerWithBank(t, "subprocess", file)
	resp, body := postJSON(t, ts.URL+"/admin/tasks/generate", token, map[string]any{"family": "shell-stats", "seed": 21, "grade": "middle"})
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ожидалось 201, получено %d: %v", resp.StatusCode, body)
	}
	id, _ := body["id"].(string)

	if _, err := os.Stat(file); err != nil {
		t.Fatalf("overlay-файл не создан: %v", err)
	}
	bank2, err := tasks.Load(file)
	if err != nil {
		t.Fatalf("повторный Load: %v", err)
	}
	if _, ok := bank2.Get(id); !ok {
		t.Fatalf("после перезагрузки задача %s пропала", id)
	}
	if bank2.GeneratedCount() != 1 {
		t.Fatalf("GeneratedCount = %d, ожидалось 1", bank2.GeneratedCount())
	}

	// Тот же seed/грейд повторно — 409, а не тихая замена задачи в банке.
	resp, body = postJSON(t, ts.URL+"/admin/tasks/generate", token, map[string]any{"family": "shell-stats", "seed": 21, "grade": "middle"})
	if resp.StatusCode != http.StatusConflict {
		t.Fatalf("повторной генерацией ожидалось 409, получено %d: %v", resp.StatusCode, body)
	}
}

// TestAdminFamiliesContract — контракт списка семей: объекты со стеками и
// грейдами. Пул (api) по этим полям выбирает, что генерировать; список строк
// ломает пополнение молча (первый же_live-прогон это и поймал).
func TestAdminFamiliesContract(t *testing.T) {
	ts, token := newTestServerWithBank(t, "subprocess", filepath.Join(t.TempDir(), "gen.json"))
	req, _ := http.NewRequest("GET", ts.URL+"/admin/families", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("families: %v", err)
	}
	defer resp.Body.Close()
	var out struct {
		Families []struct {
			ID     string   `json:"id"`
			Stacks []string `json:"stacks"`
			Grades []string `json:"grades"`
		} `json:"families"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		t.Fatalf("разбор /admin/families: %v", err)
	}
	if len(out.Families) == 0 {
		t.Fatalf("список семей пуст")
	}
	for _, f := range out.Families {
		if f.ID == "" || len(f.Stacks) == 0 || len(f.Grades) == 0 {
			t.Fatalf("семия описана неполно: %+v", f)
		}
	}
}

// TestAdminRegisterRejectsSilentTests — главное свойство gate: задача, тесты
// которой пропускают любое решение, не попадает в банк.
func TestAdminRegisterRejectsSilentTests(t *testing.T) {
	if testing.Short() {
		t.Skip("реальные прогоны bash в песочнице (не -short)")
	}
	file := filepath.Join(t.TempDir(), "gen.json")
	ts, token := newTestServerWithBank(t, "subprocess", file)

	// Чек, который всегда OK: любое решение «проходит».
	body := map[string]any{
		"id":        "silent-check-task",
		"stack":     "shell",
		"grades":    []string{"junior"},
		"title":     "Задача с вечным чеком",
		"statement": "Напечатать hello.",
		"files": map[string]string{
			"solution.sh": "#!/usr/bin/env bash\n",
			"check.sh":    "#!/usr/bin/env bash\necho OK\n",
		},
		"reference": map[string]string{"solution.sh": "#!/usr/bin/env bash\necho hello\n"},
		"mutants": []map[string]string{
			{"solution.sh": "#!/usr/bin/env bash\nexit 1\n"},
		},
	}
	resp, out := postJSON(t, ts.URL+"/admin/tasks", token, body)
	if resp.StatusCode != http.StatusUnprocessableEntity {
		t.Fatalf("ожидалось 422, получено %d: %v", resp.StatusCode, out)
	}
	raw, _ := json.Marshal(out)
	if !bytes.Contains(raw, []byte("не различают")) {
		t.Fatalf("в вердикте нет причины про тесты, которые ничего не различают: %s", raw)
	}

	// Отбракованное содержимое не должно лежать в overlay-файле.
	onDisk, err := os.ReadFile(file)
	if err == nil {
		if bytes.Contains(onDisk, []byte("silent-check-task")) {
			t.Fatalf("отбракованная задача всё-таки записана в overlay: %s", onDisk)
		}
	} else if !os.IsNotExist(err) {
		t.Fatalf("overlay: %v", err)
	}
}

// TestAdminRegisterAcceptsHonestTask — тот же путь, но тесты честные: 201, и
// задача появляется в overlay.
func TestAdminRegisterAcceptsHonestTask(t *testing.T) {
	if testing.Short() {
		t.Skip("реальные прогоны bash в песочнице (не -short)")
	}
	file := filepath.Join(t.TempDir(), "gen.json")
	ts, token := newTestServerWithBank(t, "subprocess", file)

	body := map[string]any{
		"id":        "honest-check-task",
		"stack":     "shell",
		"grades":    []string{"junior"},
		"title":     "Напечатать hello",
		"statement": "Написать решение, которое печатает ровно hello.",
		"files": map[string]string{
			"solution.sh": "#!/usr/bin/env bash\n",
			"check.sh":    "#!/usr/bin/env bash\nset -u\nout=$(bash solution.sh 2>&1)\nif [[ \"$out\" == \"hello\" ]]; then echo OK; else echo \"FAIL: $out\"; exit 1; fi\n",
		},
		"reference": map[string]string{"solution.sh": "#!/usr/bin/env bash\necho hello\n"},
		"mutants": []map[string]string{
			{"solution.sh": "#!/usr/bin/env bash\necho hellp\n"},
		},
	}
	resp, out := postJSON(t, ts.URL+"/admin/tasks", token, body)
	if resp.StatusCode != http.StatusCreated {
		t.Fatalf("ожидалось 201, получено %d: %v", resp.StatusCode, out)
	}
	onDisk, err := os.ReadFile(file)
	if err != nil {
		t.Fatalf("overlay не записан: %v", err)
	}
	if !bytes.Contains(onDisk, []byte("honest-check-task")) {
		t.Fatalf("задача не появилась в overlay: %s", onDisk)
	}
	var stored []struct {
		ID   string      `json:"id"`
		Meta *tasks.Meta `json:"meta"`
	}
	if err := json.Unmarshal(onDisk, &stored); err != nil {
		t.Fatalf("overlay не читается как банк: %v", err)
	}
	if len(stored) != 1 || stored[0].ID != "honest-check-task" {
		t.Fatalf("в overlay не та задача: %+v", stored)
	}
	if stored[0].Meta == nil || stored[0].Meta.Generator != "external" || stored[0].Meta.Hash == "" {
		t.Fatalf("в overlay нет meta (generator/hash): %+v", stored[0].Meta)
	}
}
