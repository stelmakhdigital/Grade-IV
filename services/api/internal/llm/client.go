// Package llm — OpenAI-совместимый LLM-клиент (ADR-005: vLLM в prod / llama.cpp в dev /
// мок в CI). Non-streaming Chat и стриминг ChatStream (SSE, reasoning-модели:
// delta.reasoning отбрасывается — не озвучивается и не входит в ответ).
package llm

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"
)

// Role — роль сообщения.
type Role string

const (
	RoleSystem    Role = "system"
	RoleUser      Role = "user"
	RoleAssistant Role = "assistant"
)

// Message — сообщение диалога.
type Message struct {
	Role    Role   `json:"role"`
	Content string `json:"content"`
	// Images — data-URL (base64) вложенных изображений (vision, ADR-004:
	// PNG схемы whiteboard → Qwen vision). Формат OpenAI-мультимодалки.
	Images []string `json:"images,omitempty"`
}

// Request — запрос к LLM.
type Request struct {
	Model       string    `json:"model,omitempty"`
	Messages    []Message `json:"messages"`
	Temperature float64   `json:"temperature,omitempty"`
	MaxTokens   int       `json:"max_tokens,omitempty"`
	Stream      bool      `json:"stream,omitempty"`
}

// Response — ответ LLM.
type Response struct {
	Content string
}

// Provider — абстракция LLM (подменяется мок-ом в тестах и dev-режиме, ADR-005).
type Provider interface {
	// Name — идентификатор провайдера (логи/health).
	Name() string
	// Chat — одиночный ход диалога.
	Chat(ctx context.Context, req Request) (Response, error)
	// ChatStream — стриминг хода: канал приращений content (reasoning
	// отбрасывается). Канал закрывается по [DONE]/finish. Error — только
	// ошибки запроса/соединения (поток читается из канала).
	ChatStream(ctx context.Context, req Request) (<-chan string, error)
}

// ErrLLMUnavailable — LLM-эндпоинт недоступен/ошибка HTTP (не fatal для сессии).
type ErrLLMUnavailable struct{ Err error }

func (e ErrLLMUnavailable) Error() string { return "LLM недоступна: " + e.Err.Error() }
func (e ErrLLMUnavailable) Unwrap() error { return e.Err }

// Client — HTTP-клиент OpenAI-совместимого /chat/completions.
type Client struct {
	baseURL string
	apiKey  string
	http    *http.Client
	model   string
}

// NewClient создаёт клиент (baseURL — корень, напр. http://host:8300/v1).
// Жёсткого http.Client.Timeout нет сознательно: таймаут хода задаёт ctx
// вызывающего (в стриминге он должен покрывать весь поток, а не запрос).
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{}, // таймаут задаёт вызывающий ctx
	}
}

// Name — идентификатор.
func (c *Client) Name() string { return "openai-compat:" + c.model }

// doRequest — POST {baseURL}/chat/completions (тело — сериализованный req).
func (c *Client) doRequest(ctx context.Context, req Request) (*http.Response, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, fmt.Errorf("сериализация запроса: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("запрос: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, ErrLLMUnavailable{Err: err}
	}
	return resp, nil
}

// doOnce — одиночный POST: разбор JSON-ответа, текст первого выбора и
// finish_reason (повтор при пустом контенте — на стороне Chat).
func (c *Client) doOnce(ctx context.Context, req Request) (content string, finish string, err error) {
	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return "", "", err
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return "", "", ErrLLMUnavailable{Err: fmt.Errorf("чтение ответа: %w", err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", "", ErrLLMUnavailable{Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content   string `json:"content"`
				Reasoning string `json:"reasoning"`
			} `json:"message"`
			FinishReason string `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return "", "", fmt.Errorf("разбор ответа: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return "", "", fmt.Errorf("пустой ответ LLM")
	}
	return parsed.Choices[0].Message.Content, parsed.Choices[0].FinishReason, nil
}

// Chat — POST {baseURL}/chat/completions (non-streaming).
func (c *Client) Chat(ctx context.Context, req Request) (Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	c.logf("llm: запрос model=%s msgs=%d max_tokens=%d", req.Model, len(req.Messages), req.MaxTokens)
	content, finish, err := c.doOnce(ctx, req)
	if err != nil {
		return Response{}, err
	}
	if strings.TrimSpace(content) == "" && finish == "length" {
		// Reasoning-модели (qwen3.x-hybrid и др.): при finish_reason=length весь
		// бюджет токенов уходит на рассуждение — контент пустой. Повторяем с
		// увеличенным бюджетом (рассуждение короче, контент есть).
		req.MaxTokens = req.MaxTokens * 2
		if req.MaxTokens < 1500 {
			req.MaxTokens = 1500
		}
		if content2, _, err := c.doOnce(ctx, req); err == nil {
			content = content2
		}
	}
	if strings.TrimSpace(content) == "" {
		return Response{}, ErrLLMUnavailable{Err: fmt.Errorf("LLM вернул пустой контент (finish=%s)", finish)}
	}
	c.logf("llm: ответ len=%d", len(content))
	return Response{Content: strings.TrimSpace(content)}, nil
}

// ChatStream — SSE-стриминг: POST stream:true, канал приращений content
// (delta.reasoning отбрасывается). При finish_reason=length и пустом контенте —
// повтор всего запроса в стрим-режиме с увеличенным бюджетом (как в Chat).
func (c *Client) ChatStream(ctx context.Context, req Request) (<-chan string, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	req.Stream = true
	c.logf("llm: стрим-запрос model=%s msgs=%d max_tokens=%d", req.Model, len(req.Messages), req.MaxTokens)
	resp, err := c.doRequest(ctx, req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
		resp.Body.Close()
		return nil, ErrLLMUnavailable{Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
	ch := make(chan string, 32)
	go func() {
		defer close(ch)
		defer resp.Body.Close()
		delivered, finish := c.readSSE(resp.Body, ch)
		if delivered == 0 && finish == "length" {
			// Повтор в стрим-режиме с увеличенным бюджетом (см. Chat).
			req.MaxTokens = req.MaxTokens * 2
			if req.MaxTokens < 1500 {
				req.MaxTokens = 1500
			}
			resp2, err := c.doRequest(ctx, req)
			if err != nil {
				c.logf("llm: стрим-повтор не удался: %v", err)
				return
			}
			defer resp2.Body.Close()
			c.readSSE(resp2.Body, ch)
		}
	}()
	return ch, nil
}

// readSSE — чтение SSE-потока: строки «data: …»; чанк {choices[].delta}:
// reasoning игнорируется, content уходит в ch. Возвращает (выдано байт
// content, finish_reason). Одиночная некорректная строка — пропускается.
func (c *Client) readSSE(r io.Reader, ch chan<- string) (int, string) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 64*1024), 1<<20)
	delivered := 0
	finish := ""
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if !strings.HasPrefix(line, "data:") {
			continue // комментарии/keep-alive и прочие поля SSE
		}
		payload := strings.TrimSpace(strings.TrimPrefix(line, "data:"))
		if payload == "[DONE]" {
			break
		}
		var chunk struct {
			Choices []struct {
				Delta struct {
					Reasoning string `json:"reasoning"`
					Content   string `json:"content"`
				} `json:"delta"`
				FinishReason string `json:"finish_reason"`
			} `json:"choices"`
		}
		if err := json.Unmarshal([]byte(payload), &chunk); err != nil {
			continue
		}
		for _, choice := range chunk.Choices {
			if choice.FinishReason != "" {
				finish = choice.FinishReason
			}
			if choice.Delta.Content != "" {
				ch <- choice.Delta.Content
				delivered += len(choice.Delta.Content)
			}
		}
	}
	return delivered, finish
}

// logf — лог LLM-клиента (slog, дефолтный; достаточно для диагностики).
func (c *Client) logf(format string, args ...any) {
	slog.Debug(format, args...)
}


// DefaultTimeout — верхний предел хода интервьюера (SRS: p95 хода < 4 с; запас на 4B-модель CPU).
const DefaultTimeout = 60 * time.Second
