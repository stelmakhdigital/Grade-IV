package httpapi

import (
	"errors"
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
)

// profileDTO — профиль интервьюера для REST (Итерация B).
type profileDTO struct {
	ID          int64  `json:"id"`
	Name        string `json:"name"`
	Tone        string `json:"tone"`
	Difficulty  string `json:"difficulty"`
	IsPreset    bool   `json:"is_preset"`
	Description string `json:"description"`
}

func toProfileDTO(p db.Profile) profileDTO {
	return profileDTO{ID: p.ID, Name: p.Name, Tone: p.Tone,
		Difficulty: p.Difficulty, IsPreset: p.IsPreset, Description: p.Description}
}

// handleProfilesList — GET /api/v1/profiles: профили интервьюера (Итерация B).
// Пресеты (10 шт.) сидируются при старте (ProfileStore.EnsurePresets).
func (s *Server) handleProfilesList(w http.ResponseWriter, r *http.Request) {
	list, err := s.profiles.List(r.Context())
	if err != nil {
		s.log.Error("profiles list", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить профили")
		return
	}
	out := make([]profileDTO, 0, len(list))
	for _, p := range list {
		out = append(out, toProfileDTO(p))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleProfileGet — GET /api/v1/profiles/{id}: один профиль (Итерация B).
func (s *Server) handleProfileGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	p, err := s.profiles.Get(r.Context(), id)
	if err != nil {
		if errors.Is(err, db.ErrProfileNotFound) {
			writeError(w, http.StatusNotFound, "profile_not_found", "профиль не найден")
			return
		}
		s.log.Error("profile get", "id", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить профиль")
		return
	}
	writeJSON(w, http.StatusOK, toProfileDTO(p))
}
