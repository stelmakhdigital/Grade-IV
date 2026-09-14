package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
)

// MockProvider — детерминированный провайдер для dev без LLM-узла и CI (ADR-005).
// Отвечает эхо-шаблоном; все запросы логируются (тесты проверяют промпты).
type MockProvider struct {
	mu      sync.Mutex
	calls   []Request
	respond func(req Request) (string, error) // подмена в тестах
}

// NewMockProvider — провайдер по умолчанию (эхо-ответ).
func NewMockProvider() *MockProvider {
	return &MockProvider{}
}

// Name — идентификатор.
func (m *MockProvider) Name() string { return "mock" }

// Calls — копии запросов (тесты).
func (m *MockProvider) Calls() []Request {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]Request, len(m.calls))
	copy(out, m.calls)
	return out
}

// Chat — эхо-ответ (детерминированный, без сети).
func (m *MockProvider) Chat(_ context.Context, req Request) (Response, error) {
	m.mu.Lock()
	m.calls = append(m.calls, req)
	m.mu.Unlock()

	if m.respond != nil {
		text, err := m.respond(req)
		return Response{Content: text}, err
	}
	last := ""
	if n := len(req.Messages); n > 0 {
		last = req.Messages[n-1].Content
	}
	if len(last) > 120 {
		last = last[:120] + "…"
	}
	return Response{Content: fmt.Sprintf("[mock-интервьюер] принял реплику: %s", last)}, nil
}

// SetResponder — подмена ответа (тесты: управляемый LLM).
func (m *MockProvider) SetResponder(f func(req Request) (string, error)) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.respond = f
}

// LastSystem — system-промпт последнего запроса (тесты).
func (m *MockProvider) LastSystem() string {
	m.mu.Lock()
	defer m.mu.Unlock()
	if n := len(m.calls); n > 0 {
		msgs := m.calls[n-1].Messages
		if len(msgs) > 0 && msgs[0].Role == RoleSystem {
			return msgs[0].Content
		}
	}
	return ""
}

// EnsurePrefix — тест-хелпер: последний запрос начинается с префикса.
func (m *MockProvider) EnsurePrefix(prefix string) bool {
	sys := m.LastSystem()
	return strings.HasPrefix(sys, prefix)
}
