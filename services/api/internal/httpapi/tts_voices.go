package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"sync"
)

// F1 (Итерация 10): настраиваемый реестр голосов интервьюера.
// Файл config/tts-voices.json (env TTS_VOICES_FILE — override). UI строит список
// выбора из ответа GET /api/v1/tts-voices (не хардкод). Каждый голос: id (ключ для
// UI), name (подпись), gender, speaker (реальный спикер voice-сервиса). speaker
// уходит в сессию при создании (tts_speaker) и в контекст TTS (voicesvc).
type ttsVoiceEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Gender  string `json:"gender"`
	Speaker string `json:"speaker"`
}

type ttsVoiceRegistry struct {
	Default string          `json:"default"`
	Voices  []ttsVoiceEntry `json:"voices"`
}

var (
	ttsVoicesOnce sync.Once
	ttsVoicesVal  ttsVoiceRegistry
	ttsVoicesErr  error
)

func loadTTSVoices() (ttsVoiceRegistry, error) {
	ttsVoicesOnce.Do(func() {
		path := os.Getenv("TTS_VOICES_FILE")
		if path == "" {
			path = "config/tts-voices.json"
		}
		b, err := os.ReadFile(path)
		if err != nil {
			ttsVoicesErr = err
			return
		}
		if err := json.Unmarshal(b, &ttsVoicesVal); err != nil {
			ttsVoicesErr = err
		}
	})
	return ttsVoicesVal, ttsVoicesErr
}

// handleTTSVoices — GET /api/v1/tts-voices (F1): реестр голосов интервьюера.
func (s *Server) handleTTSVoices(w http.ResponseWriter, _ *http.Request) {
	reg, err := loadTTSVoices()
	if err != nil {
		s.log.Error("tts-voices: не удалось загрузить реестр", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось загрузить реестр голосов")
		return
	}
	writeJSON(w, http.StatusOK, reg)
}
