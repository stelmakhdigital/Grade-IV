// Треки собеседований (spec-18): публичный список (для кабинета) + admin-CRUD.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// trackView — сокращённая выдача для кабинета (без промптов ревью и чек-листов:
// это серверные рубрики; UI нужны только палитра/заголовок и критерии).
type trackView struct {
	ID       string         `json:"id"`
	Title    string         `json:"title"`
	Coding   bool           `json:"coding"`
	Shell    bool           `json:"shell,omitempty"` // spec-19: практика bash (сисадмин)
	Stacks   []models.Stack `json:"stacks,omitempty"`
	Grades   []models.Grade `json:"grades"`
	Board    trackBoardView `json:"board"`
	Criteria []trackViewCri `json:"criteria"`
}

type trackBoardView struct {
	Title  string         `json:"title"`
	Groups []trackViewGrp `json:"groups"`
}

type trackViewGrp struct {
	Title string   `json:"title"`
	Names []string `json:"names"`
	Color string   `json:"color,omitempty"`
}

type trackViewCri struct {
	Name   string  `json:"name"`
	Weight float64 `json:"weight"`
}

func toTrackView(t models.Track) trackView {
	v := trackView{
		ID:     t.ID,
		Title:  t.Title,
		Coding: t.Coding,
		Shell:  t.Shell,
		Grades: t.Grades,
		Board:  trackBoardView{Title: t.Board.Title},
	}
	if t.Coding {
		v.Stacks = t.Stacks
	}
	for _, g := range t.Board.Groups {
		v.Board.Groups = append(v.Board.Groups, trackViewGrp{Title: g.Title, Names: g.Names, Color: g.Color})
	}
	for _, c := range t.Criteria {
		v.Criteria = append(v.Criteria, trackViewCri{Name: c.Name, Weight: c.Weight})
	}
	return v
}

// handleTracksList — GET /api/v1/tracks (requireAuth): список для выбора в
// кабинете. Стабильный порядок (файл, затем создания).
func (s *Server) handleTracksList(w http.ResponseWriter, _ *http.Request) {
	list := s.tracks.List()
	out := make([]trackView, 0, len(list))
	for _, t := range list {
		out = append(out, toTrackView(t))
	}
	writeJSON(w, http.StatusOK, out)
}

// handleAdminTracksList — GET /api/v1/admin/tracks: полные треки (для редактора).
func (s *Server) handleAdminTracksList(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, s.tracks.List())
}

// decodeTrack — тело admin-запроса: {"track": {...}} (канонично) либо
// «голый» объект трека. Тело буферизуется (два прохода декодера).
func decodeTrack(r *http.Request) (models.Track, error) {
	raw, err := io.ReadAll(r.Body)
	if err != nil {
		return models.Track{}, err
	}
	var wrapped struct {
		Track *models.Track `json:"track"`
	}
	if err := json.Unmarshal(raw, &wrapped); err == nil && wrapped.Track != nil {
		return *wrapped.Track, nil
	}
	var t models.Track
	if err := json.Unmarshal(raw, &t); err != nil {
		return models.Track{}, err
	}
	if t.ID == "" && t.Title == "" {
		return models.Track{}, errEmptyTrackBody
	}
	return t, nil
}

// handleAdminTrackCreate — POST /api/v1/admin/tracks {track}.
func (s *Server) handleAdminTrackCreate(w http.ResponseWriter, r *http.Request) {
	t, err := decodeTrack(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "тело: {\"track\": {...}} (spec-18)")
		return
	}
	if s.trackExists(t.ID) {
		writeError(w, http.StatusConflict, "track_exists", "трек "+t.ID+" уже есть — используй PUT")
		return
	}
	if err := s.tracks.Upsert(t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	stored, _ := s.tracks.Get(t.ID)
	writeJSON(w, http.StatusCreated, stored)
}

// handleAdminTrackUpdate — PUT /api/v1/admin/tracks/{id} {track}.
func (s *Server) handleAdminTrackUpdate(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	t, err := decodeTrack(r)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "тело: {\"track\": {...}} (spec-18)")
		return
	}
	if t.ID == "" {
		t.ID = id
	}
	if t.ID != id {
		writeError(w, http.StatusBadRequest, "invalid_request", "id в пути и теле не совпадают")
		return
	}
	if !s.trackExists(id) {
		writeError(w, http.StatusNotFound, "track_not_found", "трек "+id+" не найден")
		return
	}
	if err := s.tracks.Upsert(t); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	stored, _ := s.tracks.Get(id)
	writeJSON(w, http.StatusOK, stored)
}

// handleAdminTrackDelete — DELETE /api/v1/admin/tracks/{id}. Трек, на котором
// есть хоть одна сессия, удалять нельзя (409): история и отчёты ссылаются на
// конфиг трека; backend (дефолт совместимости) — тоже.
func (s *Server) handleAdminTrackDelete(w http.ResponseWriter, r *http.Request) {
	id := r.PathValue("id")
	if !s.trackExists(id) {
		writeError(w, http.StatusNotFound, "track_not_found", "трек "+id+" не найден")
		return
	}
	if n, err := s.sessions.CountByTrack(r.Context(), id); err == nil && n > 0 {
		writeError(w, http.StatusConflict, "track_in_use",
			"трек используется сессиями ("+strconv.FormatInt(n, 10)+") — удаление запрещено")
		return
	}
	if err := s.tracks.Delete(id); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true})
}

// trackExists — есть ли трек (для 404/409 решений обработчиков).
func (s *Server) trackExists(id string) bool {
	t, err := s.tracks.Get(id)
	return err == nil && t != nil
}

// errEmptyTrackBody — маркер пустого/нераспознанного тела.
var errEmptyTrackBody = errTrackBody{}

type errTrackBody struct{}

func (errTrackBody) Error() string { return "нет объекта трека в теле" }
