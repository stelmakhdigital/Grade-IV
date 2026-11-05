// Треки собеседований (spec-18): публичный список (для кабинета) + admin-CRUD.
package httpapi

import (
	"encoding/json"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// trackView — сокращённая выдача для кабинета (без промптов ревью и чек-листов:
// это серверные рубрики; UI нужны только палитра/заголовок и критерии).
type trackView struct {
	ID    string `json:"id"`
	Title string `json:"title"`
	// Description — 1–3 предложения о роли (кабинет: подпись под выбором).
	Description string `json:"description,omitempty"`
	// Practice — практическая стадия роли (spec-22): {runtime: go|python|shell};
	// null — без практики. Стеков «на выбор кандидата» больше нет.
	Practice *trackPracticeView `json:"practice,omitempty"`
	Grades   []models.Grade     `json:"grades"`
	Board    trackBoardView     `json:"board"`
	Criteria []trackViewCri     `json:"criteria"`
}

type trackPracticeView struct {
	Runtime models.Stack `json:"runtime"`
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
		ID:          t.ID,
		Title:       t.Title,
		Description: t.Description,
		Grades:      t.Grades,
		Board:       trackBoardView{Title: t.Board.Title},
	}
	if t.Practice != nil {
		v.Practice = &trackPracticeView{Runtime: t.Practice.Runtime}
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

// trackGenSystemPrompt — правила черновика роли для ИИ (spec-22). Жёсткие
// ограничения схемы: рантайм практики только из банков sandbox; JSON без
// обёрток и комментариев.
const trackGenSystemPrompt = `Ты конфигурируешь роль собеседования. По короткому описанию пользователя верни ТОЛЬКО JSON-объект роли без markdown и комментариев.

Схема JSON:
{
  "title": "человекочитаемое название роли (2-4 слова)",
  "description": "1-3 предложения: что за специалист и что проверяем на собеседовании",
  "practice": null | {"runtime": "go" | "python" | "shell"},
  "grades": ["junior"|"middle"|"senior"|"staff", ...],
  "board": {
    "title": "заголовок доски (например «Архитектура системы», «План проекта»)",
    "review_prompt": "что оценивать в схеме: 2-4 предложения, конкретно",
    "groups": [ {"title": "группа", "names": ["блок", "блок", ...]}, ... ],
    "cases": [ {"grade": "middle", "title": "кейс", "statement": "задание кандидату 2-3 предложения", "checklist": ["элемент", ...]}, ... ]
  },
  "criteria": [ {"name": "критерий", "weight": 0.25, "hint": "что оценивать"}, ... ]
}

Правила:
1) practice: если работа роли связана с написанием кода — {"runtime":"go"} или {"runtime":"python"} (по языку роли); для администрирования/скриптов — {"runtime":"shell"}; если код не пишут (менеджмент, аналитика без кода) — null.
2) grades: 2-3 уместных грейда из junior/middle/senior/staff.
3) board.groups: 2-4 группы по 3-6 блоков — сущности, которые кандидат рисует на доске.
4) board.cases: 1-2 кейса на middle (grade:"middle") с чек-листом из 4-6 пунктов.
5) criteria: 4-6 критериев; веса — доли, сумма ровно 1.0; последний или первый — «Коммуникация (ясность, структура)» с весом 0.10-0.15.
6) Пиши по-русски (кроме id-подобных сущностей на доске).`

// handleAdminTrackGenerate — POST /api/v1/admin/tracks/generate {description}:
// черновик роли заполняет ИИ. Черновик НЕ сохраняется — админ правит в форме
// и сохраняет обычным POST /admin/tracks. 502 — ИИ не ответил/не распарсился.
func (s *Server) handleAdminTrackGenerate(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Description string `json:"description"`
	}
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "ожидается JSON {description}")
		return
	}
	desc := strings.TrimSpace(body.Description)
	if len([]rune(desc)) < 10 {
		writeError(w, http.StatusBadRequest, "invalid_request", "опишите роль подробнее (минимум 10 символов)")
		return
	}
	if s.interviewer == nil {
		writeError(w, http.StatusBadGateway, "llm_unavailable", "ИИ недоступен — заполните роль вручную")
		return
	}
	resp, err := s.interviewer.Provider().Chat(r.Context(), llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: trackGenSystemPrompt},
			{Role: llm.RoleUser, Content: desc},
		},
		Temperature: 0.4,
		MaxTokens:   1800,
	})
	if err != nil {
		s.log.Warn("track generate: llm", "err", err)
		writeError(w, http.StatusBadGateway, "llm_unavailable", "ИИ не ответил — заполните роль вручную")
		return
	}
	var draft models.Track
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Content)), &draft); err != nil {
		s.log.Warn("track generate: парсинг", "err", err)
		writeError(w, http.StatusBadGateway, "llm_bad_json", "ИИ вернул некорректный конфиг — попробуйте ещё раз или заполните вручную")
		return
	}
	// ИИ не задаёт id (его ставит админ при сохранении) — черновик валиден
	// только по содержанию: подставляем временный slug по названию.
	if draft.ID == "" {
		draft.ID = slugifyTrack(draft.Title)
	}
	if err := draft.Validate(); err != nil {
		s.log.Warn("track generate: валидация", "err", err)
		writeError(w, http.StatusBadGateway, "llm_bad_json", "ИИ вернул некорректный конфиг — попробуйте ещё раз или заполните вручную")
		return
	}
	draft.SortCriteria()
	writeJSON(w, http.StatusOK, map[string]any{"track": draft})
}

// extractJSONObject — первый сбалансированный {...} блока текста (ответ ИИ
// может содержать обвязку «Вот конфиг:»).
func extractJSONObject(s string) string {
	start := strings.Index(s, "{")
	if start < 0 {
		return s
	}
	depth := 0
	inStr := false
	esc := false
	for i := start; i < len(s); i++ {
		c := s[i]
		switch {
		case esc:
			esc = false
		case c == '\\' && inStr:
			esc = true
		case c == '"':
			inStr = !inStr
		case inStr:
		case c == '{':
			depth++
		case c == '}':
			depth--
			if depth == 0 {
				return s[start : i+1]
			}
		}
	}
	return s[start:]
}

// translitRu — упрощённая кириллица → латиница (для черновиков id роли;
// админ правит id руками, поле в форме редактируемо).
var translitRu = map[rune]string{
	'а': "a", 'б': "b", 'в': "v", 'г': "g", 'д': "d", 'е': "e", 'ж': "zh",
	'з': "z", 'и': "i", 'й': "y", 'к': "k", 'л': "l", 'м': "m", 'н': "n",
	'о': "o", 'п': "p", 'р': "r", 'с': "s", 'т': "t", 'у': "u", 'ф': "f",
	'х': "kh", 'ц': "ts", 'ч': "ch", 'ш': "sh", 'щ': "shch", 'ъ': "", 'ы': "y",
	'ь': "", 'э': "e", 'ю': "yu", 'я': "ya", 'ё': "e",
}

// slugifyTrack — id роли из названия: латиница/цифры/дефисы; кириллица —
// транслитом, чтобы черновик от ИИ приходил с осмысленным id.
func slugifyTrack(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == ' ', r == '-', r == '_':
			if b.Len() > 0 && !strings.HasSuffix(b.String(), "-") {
				b.WriteByte('-')
			}
		default:
			if tr, ok := translitRu[r]; ok {
				b.WriteString(tr)
			}
		}
		if b.Len() >= 32 {
			break
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "role"
	}
	return out
}
