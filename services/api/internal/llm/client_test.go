package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// openAIMock — минимальный OpenAI-совместимый сервер.
func openAIMock(status int, reply string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model       string    `json:"model"`
			Messages    []Message `json:"messages"`
			Temperature float64   `json:"temperature"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if r.URL.Path != "/chat/completions" || r.Method != http.MethodPost {
			http.Error(w, "bad path", http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		if status != http.StatusOK {
			_, _ = w.Write([]byte(`{"error":"boom"}`))
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": reply}}},
		})
	}))
}

func TestClientChat(t *testing.T) {
	ts := openAIMock(http.StatusOK, "привет, как дела?")
	defer ts.Close()
	c := NewClient(ts.URL, "test-model", "secret-key")
	if c.Name() != "openai-compat:test-model" {
		t.Fatalf("name: %s", c.Name())
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := c.Chat(ctx, Request{
		Messages:    []Message{{Role: RoleUser, Content: "привет"}},
		Temperature: 0.7,
	})
	if err != nil {
		t.Fatalf("chat: %v", err)
	}
	if resp.Content != "привет, как дела?" {
		t.Fatalf("content: %q", resp.Content)
	}
}

func TestClientError(t *testing.T) {
	ts := openAIMock(http.StatusBadGateway, "")
	defer ts.Close()
	c := NewClient(ts.URL, "m", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := c.Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err == nil {
		t.Fatal("ожидалась ошибка при HTTP 502")
	}
}

func TestClientUnavailable(t *testing.T) {
	// Ничего не слушает: быстрый отказ → ErrLLMUnavailable.
	c := NewClient("http://127.0.0.1:1", "m", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err == nil {
		t.Fatal("ожидалась ошибка")
	}
}
