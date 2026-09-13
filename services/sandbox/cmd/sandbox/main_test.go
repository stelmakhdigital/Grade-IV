package main

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

func newTestServer(t *testing.T, mode string) *httptest.Server {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	bank, err := tasks.Default()
	if err != nil {
		t.Fatalf("tasks: %v", err)
	}
	rn := runner.New(runner.Config{Mode: mode})
	ts := httptest.NewServer(withLogging(buildMux(mode, bank, rn, log), log))
	t.Cleanup(ts.Close)
	return ts
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t, "subprocess")
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode: %v (body: %s)", err, raw)
	}
	if body["service"] != "grade-sandbox" {
		t.Fatalf("service = %v, want grade-sandbox", body["service"])
	}
	if body["tasks"].(float64) < 10 {
		t.Fatalf("tasks = %v, want >= 10", body["tasks"])
	}
}

func TestTasksList(t *testing.T) {
	ts := newTestServer(t, "subprocess")
	resp, err := http.Get(ts.URL + "/api/v1/tasks?stack=go")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	var list []map[string]any
	if err := json.NewDecoder(resp.Body).Decode(&list); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(list) < 5 {
		t.Fatalf("задач go: %d, want >= 5", len(list))
	}
	for _, item := range list {
		if item["stack"] != "go" || item["id"] == nil || item["files"] == nil {
			t.Fatalf("задача неполная: %v", item)
		}
	}
}

func TestRunValidation(t *testing.T) {
	ts := newTestServer(t, "subprocess")

	// Плохой стек → 400.
	body := []byte(`{"stack":"rust","action":"test","files":{"main.go":"fn main(){}"}}`)
	req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/1/runs", bytes.NewReader(body))
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("stack=rust: %d, want 400", resp.StatusCode)
	}

	// Неизвестная задача → 400.
	body = []byte(`{"stack":"go","action":"test","task_id":"nope","files":{"main.go":"x"}}`)
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/sessions/1/runs", bytes.NewReader(body))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown task: %d, want 400", resp.StatusCode)
	}

	// Пустые файлы → 400.
	body = []byte(`{"stack":"go","action":"test","files":{}}`)
	req, _ = http.NewRequest("POST", ts.URL+"/api/v1/sessions/1/runs", bytes.NewReader(body))
	resp, err = http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusBadRequest {
		t.Fatalf("empty files: %d, want 400", resp.StatusCode)
	}
}

// TestRunSubprocess — реальный dev-запуск через встраиваемую команду (не зависит от
// наличия go/pytest в CI): шуткоманда, которая «падает» и печатает.
func TestRunSubprocess(t *testing.T) {
	log := slog.New(slog.DiscardHandler)
	bank, _ := tasks.Default()
	rn := runner.New(runner.Config{
		Mode: "subprocess",
		Commands: map[string]string{
			"go":     "printf 'ok\\n' && exit 0",
			"python": "printf 'boom\\n' && exit 3",
		},
	})
	mux := buildMux("subprocess", bank, rn, log)
	ts := httptest.NewServer(withLogging(mux, log))
	t.Cleanup(ts.Close)

	do := func(stack string) map[string]any {
		t.Helper()
		body := []byte(`{"stack":"` + stack + `","action":"test","files":{"a.txt":"x"}}`)
		req, _ := http.NewRequest("POST", ts.URL+"/api/v1/sessions/9/runs", bytes.NewReader(body))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("do: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			raw, _ := io.ReadAll(resp.Body)
			t.Fatalf("status = %d (body: %s)", resp.StatusCode, raw)
		}
		var m map[string]any
		if err := json.NewDecoder(resp.Body).Decode(&m); err != nil {
			t.Fatalf("decode: %v", err)
		}
		return m
	}

	m := do("go")
	if m["exit_code"] != float64(0) || m["passed"] != true {
		t.Fatalf("go: %v", m)
	}
	if m["stdout"] != "ok\n" {
		t.Fatalf("stdout: %q", m["stdout"])
	}

	m = do("python")
	if m["exit_code"] != float64(3) || m["passed"] != false {
		t.Fatalf("python: %v", m)
	}
	if m["tests"].([]any)[0].(map[string]any)["passed"] != false {
		t.Fatalf("tests: %v", m["tests"])
	}
}
