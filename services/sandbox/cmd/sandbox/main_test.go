package main

import (
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
)

func newTestServer(t *testing.T) *httptest.Server {
	t.Helper()
	log := slog.New(slog.DiscardHandler)
	ts := httptest.NewServer(withLogging(buildMux("docker", log), log))
	t.Cleanup(ts.Close)
	return ts
}

func TestHealth(t *testing.T) {
	ts := newTestServer(t)
	resp, err := http.Get(ts.URL + "/healthz")
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d, want 200", resp.StatusCode)
	}
	var body map[string]string
	if err := json.NewDecoder(resp.Body).Decode(&body); err != nil {
		raw, _ := io.ReadAll(resp.Body)
		t.Fatalf("decode: %v (body: %s)", err, raw)
	}
	if body["service"] != "grade-sandbox" {
		t.Fatalf("service = %q, want grade-sandbox", body["service"])
	}
}

func TestRunsStub(t *testing.T) {
	ts := newTestServer(t)
	req, err := http.NewRequest("POST", ts.URL+"/api/v1/sessions/42/runs", nil)
	if err != nil {
		t.Fatalf("new request: %v", err)
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("do: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNotImplemented {
		t.Fatalf("status = %d, want 501 (stub до WP-6)", resp.StatusCode)
	}
}
