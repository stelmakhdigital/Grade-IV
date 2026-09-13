// Package voicesvc — клиент voice-сервиса (ARCHITECTURE.md §4.3): STT (multipart)
// и TTS (PCM16-ответ). MVP — TTS целиком в память (стриминг по предложениям — бэклог).
package voicesvc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"time"
)

const (
	// SampleRate — контрактный формат аудио (ADR-001).
	SampleRate = 16000
	// STTTimeout — распознавание реплики (dev CPU).
	STTTimeout = 30 * time.Second
	// TTSTimeout — синтез ответа.
	TTSTimeout  = 60 * time.Second
	maxTTSBytes = 4 << 20 // 4 МБ (~2 мин 16 кГц PCM16)
)

// STTResult — результат распознавания (§4.3).
type STTResult struct {
	Text       string  `json:"text"`
	Confidence float64 `json:"confidence"`
	DurationS  float64 `json:"duration_s"`
}

// Client — HTTP-клиент voice-сервиса.
type Client struct {
	baseURL string
	http    *http.Client
}

// NewClient создаёт клиент (baseURL — корень voice-сервиса).
func NewClient(baseURL string) *Client {
	return &Client{baseURL: baseURL, http: &http.Client{}}
}

// STT — POST /api/v1/stt: PCM16 16 кГц mono → текст. Молчание — Text "" (не ошибка).
func (c *Client) STT(ctx context.Context, pcm []byte) (STTResult, error) {
	ctx, cancel := context.WithTimeout(ctx, STTTimeout)
	defer cancel()

	var body bytes.Buffer
	mw := multipart.NewWriter(&body)
	fw, err := mw.CreateFormFile("audio", "audio.pcm")
	if err != nil {
		return STTResult{}, fmt.Errorf("multipart: %w", err)
	}
	if _, err := fw.Write(pcm); err != nil {
		return STTResult{}, fmt.Errorf("multipart audio: %w", err)
	}
	if err := mw.WriteField("sample_rate", fmt.Sprint(SampleRate)); err != nil {
		return STTResult{}, fmt.Errorf("multipart sample_rate: %w", err)
	}
	if err := mw.Close(); err != nil {
		return STTResult{}, fmt.Errorf("multipart close: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/stt", &body)
	if err != nil {
		return STTResult{}, err
	}
	req.Header.Set("Content-Type", mw.FormDataContentType())
	resp, err := c.http.Do(req)
	if err != nil {
		return STTResult{}, fmt.Errorf("voice /stt: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return STTResult{}, fmt.Errorf("voice /stt: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	var res STTResult
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return STTResult{}, fmt.Errorf("voice /stt: разбор: %w", err)
	}
	return res, nil
}

// TTS — POST /api/v1/tts: текст → PCM16 16 кГц mono (весь ответ, без WAV-заголовка).
func (c *Client) TTS(ctx context.Context, text string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, TTSTimeout)
	defer cancel()

	reqBody, err := json.Marshal(map[string]string{"text": text})
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.baseURL+"/api/v1/tts", bytes.NewReader(reqBody))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("voice /tts: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		raw, _ := io.ReadAll(io.LimitReader(resp.Body, 1024))
		return nil, fmt.Errorf("voice /tts: HTTP %d: %s", resp.StatusCode, string(raw))
	}
	pcm, err := io.ReadAll(io.LimitReader(resp.Body, maxTTSBytes))
	if err != nil {
		return nil, fmt.Errorf("voice /tts: чтение PCM: %w", err)
	}
	if len(pcm) == 0 {
		return nil, fmt.Errorf("voice /tts: пустой PCM-ответ")
	}
	return pcm, nil
}

// Healthy — GET /api/v1/health (health-check).
func (c *Client) Healthy(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.baseURL+"/api/v1/health", nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("voice /health: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("voice /health: HTTP %d", resp.StatusCode)
	}
	return nil
}
