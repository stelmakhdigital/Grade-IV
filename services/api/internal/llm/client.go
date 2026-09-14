// Package llm — OpenAI-совместимый LLM-клиент (ADR-005: vLLM в prod / llama.cpp в dev /
// мок в CI). MVP — non-streaming; стриминг — бэклог (построчный вывод интерфейра).
package llm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
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
func NewClient(baseURL, model, apiKey string) *Client {
	return &Client{
		baseURL: strings.TrimRight(baseURL, "/"),
		model:   model,
		apiKey:  apiKey,
		http:    &http.Client{}, // таймаут задаёт вызывающий ctx
	}
}

// Name — идентификатор провайдера.
func (c *Client) Name() string { return "openai-compat:" + c.model }

// Chat — POST {baseURL}/chat/completions.
func (c *Client) Chat(ctx context.Context, req Request) (Response, error) {
	if req.Model == "" {
		req.Model = c.model
	}
	body, err := json.Marshal(req)
	if err != nil {
		return Response{}, fmt.Errorf("сериализация запроса: %w", err)
	}
	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/chat/completions", bytes.NewReader(body))
	if err != nil {
		return Response{}, fmt.Errorf("запрос: %w", err)
	}
	httpReq.Header.Set("Content-Type", "application/json")
	if c.apiKey != "" {
		httpReq.Header.Set("Authorization", "Bearer "+c.apiKey)
	}
	resp, err := c.http.Do(httpReq)
	if err != nil {
		return Response{}, ErrLLMUnavailable{Err: err}
	}
	defer resp.Body.Close()
	raw, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return Response{}, ErrLLMUnavailable{Err: fmt.Errorf("чтение ответа: %w", err)}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return Response{}, ErrLLMUnavailable{Err: fmt.Errorf("HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(raw)))}
	}
	var parsed struct {
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(raw, &parsed); err != nil {
		return Response{}, fmt.Errorf("разбор ответа: %w", err)
	}
	if len(parsed.Choices) == 0 {
		return Response{}, fmt.Errorf("пустой ответ LLM")
	}
	return Response{Content: strings.TrimSpace(parsed.Choices[0].Message.Content)}, nil
}

// DefaultTimeout — верхний предел хода интервьюера (SRS: p95 хода < 4 с; запас на 4B-модель CPU).
const DefaultTimeout = 30 * time.Second
