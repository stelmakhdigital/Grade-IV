package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"strings"
	"sync"
)

// F1 (Итерация 10) + Admin (итерация 11): настраиваемый реестр голосов
// интервьюера. Файл config/tts-voices.json (env TTS_VOICES_FILE — override).
// Итерация 11: вместо sync.Once — mutex-защищённый регистр с Reload(path), чтобы
// admin-правка (PUT /api/v1/admin/tts-voices) применялась без рестарта api.

type ttsVoiceEntry struct {
	ID      string `json:"id"`
	Name    string `json:"name"`
	Gender  string `json:"gender"`
	Speaker string `json:"speaker"`
}

type ttsVoiceRegistryData struct {
	Default string          `json:"default"`
	Voices  []ttsVoiceEntry `json:"voices"`
}

// ttsVoiceRegistry — реестр в памяти + путь к файлу (mutex; итерация 11).
type ttsVoiceRegistry struct {
	mu     sync.RWMutex
	path   string
	value  ttsVoiceRegistryData
	loaded bool
	err    error
}

// resolveConfigPath — путь к config-файлу: env, иначе первый существующий
// кандидат (BUG-012), иначе базовый путь (цель для записи при admin-правке).
func resolveConfigPath(envVar, base string) string {
	if p := os.Getenv(envVar); p != "" {
		return p
	}
	if p := firstExistingConfig(configCandidates(base)); p != "" {
		return p
	}
	return base
}

func newTTSVoiceRegistry() *ttsVoiceRegistry {
	return &ttsVoiceRegistry{path: resolveConfigPath("TTS_VOICES_FILE", "config/tts-voices.json")}
}

// Get — текущий реестр (ленивая загрузка при первом чтении).
func (r *ttsVoiceRegistry) Get() (ttsVoiceRegistryData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.loaded {
		return r.value, r.err
	}
	raw, err := os.ReadFile(r.path)
	if err != nil {
		r.err = err
		return ttsVoiceRegistryData{}, err
	}
	var v ttsVoiceRegistryData
	if err := json.Unmarshal(raw, &v); err != nil {
		r.err = err
		return ttsVoiceRegistryData{}, err
	}
	r.value, r.loaded, r.err = v, true, nil
	return r.value, nil
}

// Reload — перечитать файл (итерация 11: после admin-правки). path=="" — тот же.
func (r *ttsVoiceRegistry) Reload(path string) (ttsVoiceRegistryData, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if path != "" {
		r.path = path
	}
	r.loaded = false
	r.err = nil
	raw, err := os.ReadFile(r.path)
	if err != nil {
		r.err = err
		return ttsVoiceRegistryData{}, err
	}
	var v ttsVoiceRegistryData
	if err := json.Unmarshal(raw, &v); err != nil {
		r.err = err
		return ttsVoiceRegistryData{}, err
	}
	r.value, r.loaded, r.err = v, true, nil
	return r.value, nil
}

// Path — текущий путь к файлу (для admin-записи).
func (r *ttsVoiceRegistry) Path() string {
	r.mu.RLock()
	defer r.mu.RUnlock()
	return r.path
}

// GenderOf — род голоса по спикеру (F7, итерация 12): speaker (или id) → gender.
// Неизвестный/пустой спикер или ошибка — "" (→ male в промпте, текущее поведение).
// Читает текущее значение реестра — после Reload (admin-UI) род обновляется без рестарта.
func (r *ttsVoiceRegistry) GenderOf(speaker string) string {
	sp := strings.TrimSpace(speaker)
	if sp == "" {
		return ""
	}
	data, err := r.Get()
	if err != nil {
		return ""
	}
	for _, v := range data.Voices {
		if v.Speaker == sp || v.ID == sp {
			return v.Gender
		}
	}
	return ""
}

// handleTTSVoices — GET /api/v1/tts-voices (публичный, F1): реестр голосов.
func (s *Server) handleTTSVoices(w http.ResponseWriter, _ *http.Request) {
	reg, err := s.ttsVoices.Get()
	if err != nil {
		s.log.Error("tts-voices: не удалось загрузить реестр", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось загрузить реестр голосов")
		return
	}
	writeJSON(w, http.StatusOK, reg)
}
