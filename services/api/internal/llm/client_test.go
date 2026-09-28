package llm

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
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

// sseMock — SSE-сервер: для каждого запроса отдаёт предзаданные data-строки.
func sseMock(responses ...[]string) (*httptest.Server, *int) {
	reqs := new(int)
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		i := *reqs
		*reqs++
		var req struct {
			MaxTokens int  `json:"max_tokens"`
			Stream    bool `json:"stream"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.Stream {
			http.Error(w, "ожидался stream:true", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		for _, line := range responses[min(i, len(responses)-1)] {
			_, _ = w.Write([]byte("data: " + line + "\n\n"))
		}
		if f, ok := w.(http.Flusher); ok {
			f.Flush()
		}
	})), reqs
}

func sseChunk(delta map[string]string, finish string) string {
	b, _ := json.Marshal(map[string]any{
		"object":  "chat.completion.chunk",
		"choices": []map[string]any{{"delta": delta, "finish_reason": finish}},
	})
	return string(b)
}

// collect — собрать весь канал в строку (с таймаутом от висящего потока).
func collect(t *testing.T, ch <-chan string) string {
	t.Helper()
	var sb strings.Builder
	for {
		select {
		case d, ok := <-ch:
			if !ok {
				return sb.String()
			}
			sb.WriteString(d)
		case <-time.After(5 * time.Second):
			t.Fatal("таймаут чтения SSE-канала")
		}
	}
}

func TestClientChatStream(t *testing.T) {
	ts, _ := sseMock([]string{
		// reasoning-токены — НЕ озвучиваются, в канал не попадают.
		sseChunk(map[string]string{"reasoning": "Давайте"}, ""),
		sseChunk(map[string]string{"reasoning": " подумаем..."}, ""),
		sseChunk(map[string]string{"content": "Привет, "}, ""),
		sseChunk(map[string]string{"content": "расскажите о Go."}, ""),
		sseChunk(nil, "stop"),
		"[DONE]",
	})
	defer ts.Close()
	c := NewClient(ts.URL, "m", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := c.ChatStream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "привет"}}})
	if err != nil {
		t.Fatalf("chatstream: %v", err)
	}
	got := collect(t, ch)
	if got != "Привет, расскажите о Go." {
		t.Fatalf("content: %q", got)
	}
}

func TestClientChatStreamLengthRetry(t *testing.T) {
	// Первый ход: весь бюджет на reasoning, finish_reason=length, контент пустой.
	ts, reqs := sseMock(
		[]string{sseChunk(map[string]string{"reasoning": "хм"}, ""), sseChunk(nil, "length")},
		[]string{
			sseChunk(map[string]string{"content": "О, теперь есть "}, ""),
			sseChunk(map[string]string{"content": "контент."}, "stop"),
			"[DONE]",
		},
	)
	defer ts.Close()
	c := NewClient(ts.URL, "m", "")
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	ch, err := c.ChatStream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}, MaxTokens: 400})
	if err != nil {
		t.Fatalf("chatstream: %v", err)
	}
	got := collect(t, ch)
	if got != "О, теперь есть контент." {
		t.Fatalf("content после length-повтора: %q", got)
	}
	if *reqs != 2 {
		t.Fatalf("ожидается 2 запроса (повтор), фактически %d", *reqs)
	}
}

func TestClientChatStreamUnavailable(t *testing.T) {
	c := NewClient("http://127.0.0.1:1", "m", "")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := c.ChatStream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err == nil {
		t.Fatal("ожидалась ошибка соединения")
	}
}

// TestClientChatTemplateKwargs — тело запроса содержит верхнеуровневое поле
// chat_template_kwargs.enable_thinking с нужным значением (vLLM) — и в
// non-streaming Chat, и в ChatStream. Фейковый сервер читает тело.
func TestClientChatTemplateKwargs(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Stream             bool `json:"stream"`
			ChatTemplateKwargs struct {
				EnableThinking bool `json:"enable_thinking"`
			} `json:"chat_template_kwargs"`
		}
		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			http.Error(w, err.Error(), http.StatusBadRequest)
			return
		}
		if req.ChatTemplateKwargs.EnableThinking {
			t.Errorf("enable_thinking: want false (клиент без WithEnableThinking), got true")
		}
		if req.Stream {
			w.Header().Set("Content-Type", "text/event-stream")
			_, _ = w.Write([]byte("data: " + sseChunk(map[string]string{"content": "ок"}, "stop") + "\n\ndata: [DONE]\n\n"))
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "ок"}}},
		})
	}))
	defer srv.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	c := NewClient(srv.URL, "m", "")
	if _, err := c.Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("chat: %v", err)
	}
	ch, err := c.ChatStream(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}})
	if err != nil {
		t.Fatalf("chatstream: %v", err)
	}
	if got := collect(t, ch); got != "ок" {
		t.Fatalf("stream content: %q", got)
	}

	// Явно включённое thinking — true в теле.
	srv2 := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			ChatTemplateKwargs struct {
				EnableThinking bool `json:"enable_thinking"`
			} `json:"chat_template_kwargs"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		if !req.ChatTemplateKwargs.EnableThinking {
			t.Errorf("enable_thinking: want true (WithEnableThinking(true)), got false")
		}
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"choices": []map[string]any{{"message": map[string]string{"content": "ок"}}},
		})
	}))
	defer srv2.Close()
	if _, err := NewClient(srv2.URL, "m", "").WithEnableThinking(true).
		Chat(ctx, Request{Messages: []Message{{Role: RoleUser, Content: "x"}}}); err != nil {
		t.Fatalf("chat (thinking on): %v", err)
	}
}

func TestMockProviderChatStream(t *testing.T) {
	m := NewMockProvider()
	ch, err := m.ChatStream(context.Background(), Request{Messages: []Message{{Role: RoleUser, Content: "привет"}}})
	if err != nil {
		t.Fatalf("mock chatstream: %v", err)
	}
	got := collect(t, ch)
	if !strings.Contains(got, "привет") {
		t.Fatalf("mock-ответ: %q", got)
	}
}
