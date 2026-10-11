package httpapi

import (
	"encoding/json"
	"net/http"
)

// maxVacancyLen — лимит текста вакансии (F6, итерация 12): 20000 символов.
const maxVacancyLen = 20000

// vacancyReq — тело PUT /me/vacancy.
type vacancyReq struct {
	VacancyText string `json:"vacancy_text"`
}

// handleGetVacancy — GET /api/v1/me/vacancy → {vacancy_text}.
func (s *Server) handleGetVacancy(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	text, err := s.users.GetVacancy(r.Context(), uid)
	if err != nil {
		s.log.Error("get vacancy", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения вакансии")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"vacancy_text": text})
}

// handlePutVacancy — PUT /api/v1/me/vacancy, body {vacancy_text} (≤ 20000 символов;
// пустая строка = сброс). Возвращает сохранённое состояние.
func (s *Server) handlePutVacancy(w http.ResponseWriter, r *http.Request) {
	var req vacancyReq
	if err := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20)).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request", "некорректный JSON: "+err.Error())
		return
	}
	if n := len([]rune(req.VacancyText)); n > maxVacancyLen {
		writeError(w, http.StatusBadRequest, "too_long", "вакансия длиннее 20000 символов")
		return
	}
	uid := userIDFromContext(r.Context())
	if err := s.users.SetVacancy(r.Context(), uid, req.VacancyText); err != nil {
		s.log.Error("set vacancy", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка сохранения вакансии")
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"vacancy_text": req.VacancyText})
}
