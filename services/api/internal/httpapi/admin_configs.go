package httpapi

import (
	"encoding/json"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
)

// Admin: редактирование набора начал + реестра TTS-голосов (итерация 11).
// PUT — атомарная запись файла (tmp+rename в том же каталоге) + reload в памяти
// (без рестарта api). Путь — тот же, из которого файл загружен (env/кандидат, BUG-012).

const maxPromptLen = 2000

var validOpeningsGrades = map[string]bool{
	"junior": true, "middle": true, "senior": true, "staff": true, "*": true,
}

// writeAtomic — атомарная запись файла (tmp + rename).
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if dir == "" || dir == "." {
		dir = "."
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".cfg-*.tmp")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName) // no-op после успешного rename
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	if err := os.Chmod(tmpName, 0o644); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}

// ---------- Openings ----------

type openingDTO struct {
	Grade  string `json:"grade"`
	Prompt string `json:"prompt"`
}

// handleAdminOpenings — GET /api/v1/admin/openings: текущий набор (из памяти).
func (s *Server) handleAdminOpenings(w http.ResponseWriter, _ *http.Request) {
	list := s.interviewer.CurrentOpenings()
	if list == nil {
		list = []interviewer.Opening{}
	}
	writeJSON(w, http.StatusOK, list)
}

// handleAdminOpeningsPut — PUT /api/v1/admin/openings: запись полного списка.
func (s *Server) handleAdminOpeningsPut(w http.ResponseWriter, r *http.Request) {
	var req []openingDTO
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"некорректное JSON-тело (ожидался массив [{grade, prompt}])")
		return
	}
	var list []interviewer.Opening
	for i, o := range req {
		if !validOpeningsGrades[o.Grade] {
			writeError(w, http.StatusBadRequest, "validation",
				"строка "+strconv.Itoa(i+1)+": некорректный grade (junior/middle/senior/staff/*)")
			return
		}
		p := strings.TrimSpace(o.Prompt)
		if p == "" {
			writeError(w, http.StatusBadRequest, "validation", "строка "+strconv.Itoa(i+1)+": prompt пустой")
			return
		}
		if len([]rune(p)) > maxPromptLen {
			writeError(w, http.StatusBadRequest, "validation", "строка "+strconv.Itoa(i+1)+": prompt длиннее "+strconv.Itoa(maxPromptLen)+" символов")
			return
		}
		list = append(list, interviewer.Opening{Grade: o.Grade, Prompt: p})
	}
	content, err := json.MarshalIndent(struct {
		Openings []interviewer.Opening `json:"openings"`
	}{list}, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "ошибка сериализации")
		return
	}
	path := s.openingsPath
	if err := writeAtomic(path, content); err != nil {
		s.log.Error("admin openings: запись файла", "err", err)
		writeError(w, http.StatusInternalServerError, "io_error", "ошибка записи файла: "+err.Error())
		return
	}
	s.interviewer.SetOpenings(interviewer.OpeningsFromList(list)) // reload в памяти
	s.log.Info("admin: набор начал обновлён", "path", path, "count", len(list))
	writeJSON(w, http.StatusOK, list)
}

// ---------- TTS-voices ----------

// handleAdminTTSVoices — GET /api/v1/admin/tts-voices: текущий реестр.
func (s *Server) handleAdminTTSVoices(w http.ResponseWriter, _ *http.Request) {
	reg, err := s.ttsVoices.Get()
	if err != nil {
		s.log.Error("admin tts-voices: чтение", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения реестра голосов")
		return
	}
	writeJSON(w, http.StatusOK, reg)
}

// handleAdminTTSVoicesPut — PUT /api/v1/admin/tts-voices: запись полного реестра.
func (s *Server) handleAdminTTSVoicesPut(w http.ResponseWriter, r *http.Request) {
	var reg ttsVoiceRegistryData
	if err := json.NewDecoder(r.Body).Decode(&reg); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"некорректное JSON-тело (ожидался {default, voices:[{id,name,gender,speaker}]})")
		return
	}
	if len(reg.Voices) == 0 {
		writeError(w, http.StatusBadRequest, "validation", "реестр должен содержать хотя бы один голос")
		return
	}
	speakerSet := map[string]bool{}
	for i, v := range reg.Voices {
		reg.Voices[i].ID = strings.TrimSpace(v.ID)
		reg.Voices[i].Speaker = strings.TrimSpace(v.Speaker)
		reg.Voices[i].Name = strings.TrimSpace(v.Name)
		reg.Voices[i].Gender = strings.TrimSpace(v.Gender)
		if reg.Voices[i].ID == "" {
			writeError(w, http.StatusBadRequest, "validation", "голос "+strconv.Itoa(i+1)+": id пустой")
			return
		}
		if reg.Voices[i].Speaker == "" {
			writeError(w, http.StatusBadRequest, "validation", "голос "+strconv.Itoa(i+1)+": speaker пустой")
			return
		}
		speakerSet[reg.Voices[i].Speaker] = true
	}
	def := strings.TrimSpace(reg.Default)
	if def == "" {
		def = reg.Voices[0].Speaker
	}
	if !speakerSet[def] {
		writeError(w, http.StatusBadRequest, "validation", "default не входит в список speaker голосов")
		return
	}
	reg.Default = def

	content, err := json.MarshalIndent(reg, "", "  ")
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "ошибка сериализации")
		return
	}
	path := s.ttsVoices.Path()
	if err := writeAtomic(path, content); err != nil {
		s.log.Error("admin tts-voices: запись файла", "err", err)
		writeError(w, http.StatusInternalServerError, "io_error", "ошибка записи файла: "+err.Error())
		return
	}
	newReg, err := s.ttsVoices.Reload(path)
	if err != nil {
		s.log.Error("admin tts-voices: reload", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка перезагрузки реестра")
		return
	}
	s.log.Info("admin: реестр голосов обновлён", "path", path, "count", len(newReg.Voices))
	writeJSON(w, http.StatusOK, newReg)
}
