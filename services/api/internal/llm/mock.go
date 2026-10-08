package llm

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"
)

// MockProvider — детерминированный провайдер для dev без LLM-узла и CI (ADR-005).
// Отвечает эхо-шаблоном; все запросы логируются (тесты проверяют промпты).
type MockProvider struct {
	mu             sync.Mutex
	calls          []Request
	respond        func(req Request) (string, error) // подмена в тестах
	streamReqDelay time.Duration                     // тесты: задержка ChatStream (LLM-фаза)
	tokenDelay     time.Duration                     // тесты: задержка между токенами в ChatStream-стриме (0 — весь ответ одним токеном)
	streamEnd      time.Time                         // тесты: момент окончания последней ChatStream-поставки
}

// SetStreamReqDelay — искусственная задержка ChatStream ДО возврата канала
// (тесты: моделирование LLM-фазы — медленный запрос до первого байта; окно,
// в котором ход прошёл статус-гард, но TTS ещё не начался).
func (m *MockProvider) SetStreamReqDelay(d time.Duration) {
	m.mu.Lock()
	m.streamReqDelay = d
	m.mu.Unlock()
}

// SetTokenDelay — искусственная задержка МЕЖДУ токенами в ChatStream-стриме
// (тесты: медленный LLM, моделирование ~10–25 ток/с). По умолчанию 0 —
// весь ответ одним токеном (как раньше).
func (m *MockProvider) SetTokenDelay(d time.Duration) {
	m.mu.Lock()
	m.tokenDelay = d
	m.mu.Unlock()
}

// StreamEnd — момент, когда последний ChatStream завершил поставку токенов
// (тесты: клиуза-диспетчизация — TTS-вызов должен предшествовать концу стрима).
// Zero — стримов не было.
func (m *MockProvider) StreamEnd() time.Time {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.streamEnd
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

// ChatStream — стриминг поверх Chat: весь ответ одной фразой в канал
// (SetStreamReqDelay: искусственная задержка запроса до первого байта — тесты
// LLM-фазы; SetTokenDelay: задержка между токенами — тесты медленного LLM).
func (m *MockProvider) ChatStream(ctx context.Context, req Request) (<-chan string, error) {
	m.mu.Lock()
	delay := m.streamReqDelay
	tokenDelay := m.tokenDelay
	m.mu.Unlock()
	if delay > 0 {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(delay):
		}
	}
	resp, err := m.Chat(ctx, req)
	if err != nil {
		return nil, err
	}
	ch := make(chan string, 1)
	go func() {
		defer close(ch)
		defer func() {
			m.mu.Lock()
			m.streamEnd = time.Now()
			m.mu.Unlock()
		}()
		if tokenDelay > 0 {
			for _, tok := range splitTokens(resp.Content) {
				select {
				case ch <- tok:
				case <-ctx.Done():
					return
				}
				select {
				case <-time.After(tokenDelay):
				case <-ctx.Done():
					return
				}
			}
			return
		}
		ch <- resp.Content
	}()
	return ch, nil
}

// splitTokens — текст → токены (слово + за ним whitespace) для имитации
// LLM-стрима (SetTokenDelay): склейка токенов даёт исходный текст без потерь.
func splitTokens(s string) []string {
	var toks []string
	for len(s) > 0 {
		i := strings.IndexAny(s, " \t\n")
		if i < 0 {
			toks = append(toks, s)
			break
		}
		j := i
		for j < len(s) && (s[j] == ' ' || s[j] == '\t' || s[j] == '\n') {
			j++
		}
		toks = append(toks, s[:j])
		s = s[j:]
	}
	return toks
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
