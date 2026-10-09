package httpapi

import (
	"context"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/config"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// TestNewMockProvider_FixedResponse — LLM_MOCK_RESPONSE: фиксированный ответ
// (детерминированный замер, T-20261009001516) вместо эхо.
func TestNewMockProvider_FixedResponse(t *testing.T) {
	cfg := &config.Config{LLMMock: true, LLMMockResponse: "Фиксированный ответ для замера."}
	p := newMockProvider(cfg)
	if _, err := p.Chat(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}}); err != nil {
		t.Fatalf("Chat: %v", err)
	}
	res, err := p.Chat(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Content != "Фиксированный ответ для замера." {
		t.Fatalf("ответ != фиксированному: %q", res.Content)
	}
}

// TestNewMockProvider_GreetingShort — приветствие (system-промпт «поприветствуй»)
// — короткий ответ (ускоряет замер), а на ход — фиксированный ответ.
func TestNewMockProvider_GreetingShort(t *testing.T) {
	cfg := &config.Config{LLMMock: true, LLMMockResponse: "Длинный фиксированный ответ."}
	p := newMockProvider(cfg)
	greet, _ := p.Chat(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "Кандидат только что начал интервью. Кратко поприветствуй."},
		{Role: llm.RoleUser, Content: "..."},
	}})
	if greet.Content != "Привет. Давайте начнём." {
		t.Fatalf("greeting != короткому: %q", greet.Content)
	}
	ans, _ := p.Chat(context.Background(), llm.Request{Messages: []llm.Message{
		{Role: llm.RoleSystem, Content: "Ты инженер-интервьюер."},
		{Role: llm.RoleUser, Content: "решение кандидата"},
	}})
	if ans.Content != "Длинный фиксированный ответ." {
		t.Fatalf("ответ != фиксированному: %q", ans.Content)
	}
}

// TestNewMockProvider_TokenStream — LLM_MOCK_TOKENS_PER_S: стриминг по токенам
// (эмуляция скорости реального узла): ответ уходит несколькими токенами
// (по словам) с задержкой, а не одним куском.
func TestNewMockProvider_TokenStream(t *testing.T) {
	// 5 слов @ 100 ток/с = 5 токенов по 10 мс.
	cfg := &config.Config{LLMMock: true, LLMMockResponse: "раз два три четыре пять", LLMMockTokensPerS: 100}
	p := newMockProvider(cfg)
	ch, err := p.ChatStream(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "x"}}})
	if err != nil {
		t.Fatalf("ChatStream: %v", err)
	}
	var toks []string
	for tok := range ch {
		toks = append(toks, tok)
	}
	if len(toks) != 5 {
		t.Fatalf("ожидали 5 токенов (по словам), получено %d: %v", len(toks), toks)
	}
}

// TestNewMockProvider_DefaultEcho — без LLM_MOCK_RESPONSE — эхо (поведение по
// умолчанию не изменилось).
func TestNewMockProvider_DefaultEcho(t *testing.T) {
	cfg := &config.Config{LLMMock: true}
	p := newMockProvider(cfg)
	res, err := p.Chat(context.Background(), llm.Request{Messages: []llm.Message{{Role: llm.RoleUser, Content: "привет"}}})
	if err != nil {
		t.Fatalf("Chat: %v", err)
	}
	if res.Content == "" {
		t.Fatal("пустой эхо-ответ")
	}
}

// TestConfig_LLMMockEnv — LLM_MOCK_RESPONSE / LLM_MOCK_TOKENS_PER_S читаются из env.
func TestConfig_LLMMockEnv(t *testing.T) {
	t.Setenv("LLM_MOCK_RESPONSE", "текст из env")
	t.Setenv("LLM_MOCK_TOKENS_PER_S", "30.5")
	c, err := config.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if c.LLMMockResponse != "текст из env" {
		t.Fatalf("LLMMockResponse = %q", c.LLMMockResponse)
	}
	if c.LLMMockTokensPerS != 30.5 {
		t.Fatalf("LLMMockTokensPerS = %v", c.LLMMockTokensPerS)
	}
	// Скорость → задержка на токен: 1/30.5 с ≈ 32.78 мс.
	if d := time.Duration(float64(time.Second) / c.LLMMockTokensPerS); d < 32*time.Millisecond || d > 34*time.Millisecond {
		t.Fatalf("неверная задержка на токен: %v", d)
	}
}
