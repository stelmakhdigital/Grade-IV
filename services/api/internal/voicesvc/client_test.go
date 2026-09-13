package voicesvc

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestClientSTT(t *testing.T) {
	var gotAudio, gotRate string
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/stt" {
			http.NotFound(w, r)
			return
		}
		_ = r.ParseMultipartForm(1 << 20)
		f, _, err := r.FormFile("audio")
		if err != nil {
			http.Error(w, "no audio: "+err.Error(), http.StatusBadRequest)
			return
		}
		raw, _ := io.ReadAll(f)
		gotAudio = string(raw[:min(2, len(raw))])
		_ = gotAudio
		gotRate = r.FormValue("sample_rate")
		_ = f.Close()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"привет, интервьюер","confidence":0.91,"duration_s":1.2}`))
	}))
	defer ts.Close()

	c := NewClient(ts.URL)
	res, err := c.STT(context.Background(), []byte{1, 2, 3, 4})
	if err != nil {
		t.Fatalf("stt: %v", err)
	}
	if res.Text != "привет, интервьюер" || res.Confidence != 0.91 || res.DurationS != 1.2 {
		t.Fatalf("результат: %+v", res)
	}
	if gotRate != "16000" {
		t.Fatalf("sample_rate: %q", gotRate)
	}
}

func TestClientSTTError(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadGateway)
		_, _ = w.Write([]byte(`{"detail":"boom"}`))
	}))
	defer ts.Close()
	if _, err := NewClient(ts.URL).STT(context.Background(), []byte{1}); err == nil {
		t.Fatal("ожидалась ошибка при HTTP 502")
	}
}

func TestClientSTTEmptyText(t *testing.T) {
	// Молчание — text="" (200, не ошибка; решение контракта §4.3).
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"text":"","confidence":0.0,"duration_s":2.0}`))
	}))
	defer ts.Close()
	res, err := NewClient(ts.URL).STT(context.Background(), make([]byte, 64000))
	if err != nil {
		t.Fatalf("stt: %v", err)
	}
	if res.Text != "" {
		t.Fatalf("молчание: text=%q", res.Text)
	}
}

func TestClientTTS(t *testing.T) {
	pcm := make([]byte, 6400) // 0.2 с 16 кГц PCM16
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/tts" {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "audio/pcm")
		w.Header().Set("X-Sample-Rate", "16000")
		_, _ = w.Write(pcm)
	}))
	defer ts.Close()

	out, err := NewClient(ts.URL).TTS(context.Background(), "привет")
	if err != nil {
		t.Fatalf("tts: %v", err)
	}
	if len(out) != len(pcm) {
		t.Fatalf("длина PCM: %d (ожидалось %d)", len(out), len(pcm))
	}
}

func TestClientTTSBadRequest(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"code":"empty_text","message":"текст пуст"}`))
	}))
	defer ts.Close()
	if _, err := NewClient(ts.URL).TTS(context.Background(), ""); err == nil {
		t.Fatal("ожидалась ошибка на пустом тексте")
	}
}

func TestClientHealthy(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"service":"grade-voice"}`))
	}))
	defer ts.Close()
	if err := NewClient(ts.URL).Healthy(context.Background()); err != nil {
		t.Fatalf("healthy: %v", err)
	}
}
