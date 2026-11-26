// Резюме кандидата: загрузка профиля (описание + файл), и подбор роли/грейда
// по нему. Кабинет больше не спрашивает роль и грейд — их определяет ИИ по
// резюме: роль берётся из настроенных (config/roles.json), а если подходящей
// нет — ИИ генерирует конфиг роли по тому же шаблону, что и admin-generate.
package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/resume"
)

// maxResumeAboutLen — лимит текста «о себе» (как вакансия — 20000 символов).
const maxResumeAboutLen = 20000

// resumeOut — ответ GET/PUT /me/resume. Полный извлечённый текст наружу не
// отдаём: он нужен в промпте, кандидату достаточнофакта «файл прочитан, N символов».
type resumeOut struct {
	About     string `json:"about"`
	FileName  string `json:"file_name"`
	TextChars int    `json:"text_chars"`
}

func toResumeOut(r db.Resume) resumeOut {
	return resumeOut{About: r.About, FileName: r.FileName, TextChars: len([]rune(r.Text))}
}

// handleGetResume — GET /api/v1/me/resume.
func (s *Server) handleGetResume(w http.ResponseWriter, r *http.Request) {
	res, err := s.users.GetResume(r.Context(), userIDFromContext(r.Context()))
	if err != nil {
		s.log.Error("get resume", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения резюме")
		return
	}
	writeJSON(w, http.StatusOK, toResumeOut(res))
}

// handlePutResume — PUT /api/v1/me/resume (multipart: поле about + файл resume).
// Файл опционален: без него сохраняется только описание, приложенный ранее файл
// остаётся. Форматы — pdf/docx/doc (≤ 8 МБ), текст вытаскивается на сервере.
func (s *Server) handlePutResume(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	if err := r.ParseMultipartForm(int64(resume.MaxBytes) + (1 << 20)); err != nil {
		writeError(w, http.StatusBadRequest, "bad_request",
			"ожидается multipart/form-data (поля about и/или файл resume)")
		return
	}
	defer r.MultipartForm.RemoveAll()

	res, err := s.users.GetResume(r.Context(), uid)
	if err != nil {
		s.log.Error("get resume", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения резюме")
		return
	}
	// Поля about в форме нет — описание не трогаем (чтобы правка файла не затирала текст).
	if _, ok := r.MultipartForm.Value["about"]; ok {
		about := strings.TrimSpace(r.FormValue("about"))
		if n := len([]rune(about)); n > maxResumeAboutLen {
			writeError(w, http.StatusBadRequest, "too_long", "описание длиннее 20000 символов")
			return
		}
		res.About = about
	}

	fh, fhdr, err := r.FormFile("resume")
	if err != nil {
		s.saveResume(w, r.Context(), uid, res) // файл не присылали
		return
	}
	defer fh.Close()
	if !resume.Supported(fhdr.Filename) {
		writeError(w, http.StatusBadRequest, "unsupported_format", "поддерживаются pdf, docx и doc")
		return
	}
	data, err := io.ReadAll(io.LimitReader(fh, resume.MaxBytes+1))
	if err != nil {
		s.log.Warn("resume: загрузка не прочитана", "err", err)
		writeError(w, http.StatusBadRequest, "bad_request", "файл не прочитан, попробуй приложить его заново")
		return
	}
	if len(data) == 0 {
		writeError(w, http.StatusBadRequest, "empty_file", "файл пустой")
		return
	}
	if len(data) > resume.MaxBytes {
		writeError(w, http.StatusBadRequest, "too_large", "файл больше 8 МБ")
		return
	}
	text, err := resume.Extract(fhdr.Filename, data)
	if err != nil {
		writeError(w, http.StatusBadRequest, "unreadable_file", "не удалось прочитать файл ("+
			err.Error()+") — приложите pdf/docx или опишите опыт текстом")
		return
	}
	path, err := s.storeResumeFile(uid, fhdr.Filename, data)
	if err != nil {
		s.log.Error("store resume file", "err", err)
		writeError(w, http.StatusInternalServerError, "io_error", "не удалось сохранить файл")
		return
	}
	if res.Path != "" && res.Path != path {
		_ = os.Remove(res.Path) // старый файл этого же пользователя
	}
	res.Text = resume.Clamp(text)
	res.FileName = sanitizeFileName(fhdr.Filename)
	res.Path = path
	s.saveResume(w, r.Context(), uid, res)
}

// handleDeleteResume — DELETE /api/v1/me/resume: сброс описания, текста и файла.
func (s *Server) handleDeleteResume(w http.ResponseWriter, r *http.Request) {
	uid := userIDFromContext(r.Context())
	res, err := s.users.GetResume(r.Context(), uid)
	if err != nil {
		s.log.Error("get resume", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения резюме")
		return
	}
	if res.Path != "" {
		_ = os.Remove(res.Path)
	}
	if err := s.users.SetResume(r.Context(), uid, db.Resume{}); err != nil {
		s.log.Error("reset resume", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сбросить резюме")
		return
	}
	writeJSON(w, http.StatusOK, resumeOut{})
}

// saveResume — сохранить профиль и отдать состояние кандидату.
func (s *Server) saveResume(w http.ResponseWriter, ctx context.Context, uid int64, res db.Resume) {
	if err := s.users.SetResume(ctx, uid, res); err != nil {
		s.log.Error("set resume", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "не удалось сохранить резюме")
		return
	}
	writeJSON(w, http.StatusOK, toResumeOut(res))
}

// storeResumeFile — положить файл в cfg.ResumeDir под серверным именем
// (id + случай: чужое имя не используется, перезапись чужого файла невозможна).
func (s *Server) storeResumeFile(uid int64, origName string, data []byte) (string, error) {
	dir := s.cfg.ResumeDir
	if dir == "" {
		dir = "./data/resumes"
	}
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return "", err
	}
	var rnd [6]byte
	if _, err := rand.Read(rnd[:]); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%s%s", uid, hex.EncodeToString(rnd[:]), strings.ToLower(filepath.Ext(origName)))
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, data, 0o640); err != nil {
		return "", err
	}
	return path, nil
}

// sanitizeFileName — имя файла только для отображения (путь и каталоги вырезаются).
func sanitizeFileName(name string) string {
	name = filepath.Base(strings.ReplaceAll(name, "\\", "/"))
	if name == "." || name == "/" || name == "" {
		return "resume"
	}
	return name
}

// ------------------------------------------------------ роль и базовый грейд по резюме

// roleGuess — ответ ИИ на подбор роли и грейда по резюме.
type roleGuess struct {
	TrackID string         `json:"track_id"`
	Grade   string         `json:"grade"`
	Reason  string         `json:"reason"`
	Title   string         `json:"title"`
	Stacks  []models.Stack `json:"stacks"`
	NewRole *models.Track  `json:"new_track,omitempty"`
}

// roleFitPrompt — системный промпт подбора: список настроенных ролей + правила
// генерации новой (те же поля, что в trackGenSystemPrompt).
func roleFitPrompt(roles []models.Track) string {
	var b strings.Builder
	b.WriteString("Кандидат прислал резюме. Выбери роль собеседования и базовый грейд.\n\n" +
		"Роли, уже настроенные в системе (id — значение track_id):\n")
	for _, t := range roles {
		fmt.Fprintf(&b, "- %s — %s. %s\n", t.ID, t.Title, strings.TrimSpace(t.Description))
	}
	b.WriteString(`
Новая роль (new_track) — объект с полями:
{"id":"slug","title":"…","description":"1-3 предложения","practice":{"runtime":"go|python|shell"}|null,
 "grades":["junior","middle","senior"],
 "board":{"title":"…","review_prompt":"…","groups":[{"title":"…","names":["…"]}],"cases":[{"grade":"middle","title":"…","statement":"…","checklist":["…"]}]},
 "criteria":[{"name":"…","weight":0.25,"hint":"…"}]}
Правила new_track: grades — 2-3 уместных грейда; board.groups — 2-4 группы по 3-6 блоков;
board.cases — 1-2 кейса для middle; criteria — 4-6 критериев, веса — доли с суммой ровно 1.0,
один из них «Коммуникация (ясность, структура)» с весом 0.10-0.15; тексты на русском.

Ответь СТРОГО JSON без пояснений:
{"track_id":"…","grade":"junior|middle|senior|staff","reason":"1 предложение","title":"…","stacks":["python","go"],"new_track":{…}|null

Правила выбора:
1) track_id — id роли из списка, если она подходит по сути задач (не по названию).
2) Подходящей роли нет — track_id короткий slug новой роли (строчные латиница/цифры/дефис, 2-32),
   new_track — полный конфиг, описанный выше.
3) grade — грейд, который резюме подтверждает фактами (опыт, проекты, глубина). Не завышай:
   2 года CRUD-а — middle, а не senior. Резюме пустое или нечитаемое — middle.
4) reason — выбор по фактам резюме.
5) title — должность так, как её видно из резюме, на русском: «Python-разработчик»,
   «Fullstack-разработчик» (если тянет и на фронт, и на бэк), «Системный администратор».
   Роль из списка — только про формат интервью; названием интервью она быть не обязана.
6) stacks — языки, по которым кандидат реально писал код в опыте (не «знаком»),
   по убыванию веса. Разрешены: go, python, shell. Спрашивать будем по каждому
   из них, а кодить — по первому. Роль без кода — [].`)
	return b.String()
}

// resumeProfile — что ИИ вычитал из резюме: роль, базовый грейд, должность и
// заявленные языки. Всё, кроме TrackID, может быть пустым (нет резюме, ИИ
// не ответил, ответ не разобрался).
type resumeProfile struct {
	TrackID string
	Base    models.Grade
	Title   string
	Stacks  []models.Stack
}

// inferRole — роль, базовый грейд, должность и языки по резюме кандидата (ИИ).
// Пустой Base — определять было нечего (резюме нет) или ИИ не помог: тогда
// грейд берётся дефолтный, а «шаг выше» не выдумывается.
func (s *Server) inferRole(ctx context.Context, userID int64) resumeProfile {
	res, err := s.users.GetResume(ctx, userID)
	if err != nil || res.Empty() || s.interviewer == nil {
		return resumeProfile{TrackID: models.DefaultTrackID}
	}
	list := s.tracks.List()
	resp, err := s.interviewer.Provider().Chat(ctx, llm.Request{
		Messages: []llm.Message{
			{Role: llm.RoleSystem, Content: roleFitPrompt(list)},
			{Role: llm.RoleUser, Content: "Резюме кандидата:\n\"\"\"\n" + res.Prompt() + "\n\"\"\""},
		},
		Temperature: 0.2,
		MaxTokens:   2200,
	})
	if err != nil {
		s.log.Warn("resume: ИИ не ответил — роль backend, грейд по умолчанию", "err", err)
		return resumeProfile{TrackID: models.DefaultTrackID}
	}
	var g roleGuess
	if err := json.Unmarshal([]byte(extractJSONObject(resp.Content)), &g); err != nil {
		s.log.Warn("resume: ответ ИИ не разобран — роль backend, грейд по умолчанию", "err", err)
		return resumeProfile{TrackID: models.DefaultTrackID}
	}
	base := models.Grade(strings.ToLower(strings.TrimSpace(g.Grade)))
	if !base.Valid() {
		base = ""
	}
	stacks := cleanStacks(g.Stacks)
	title := strings.TrimSpace(g.Title)
	id := strings.ToLower(strings.TrimSpace(g.TrackID))
	if id == "" {
		return resumeProfile{TrackID: models.DefaultTrackID, Base: base, Title: title, Stacks: stacks}
	}
	if t, _ := s.tracks.Get(id); t != nil {
		if title == "" {
			title = t.Title // должностью из резюме пренебрегать нечем
		}
		s.log.Info("resume: роль по резюме", "track", id, "grade", base,
			"title", title, "stacks", joinStacks(stacks), "reason", g.Reason)
		return resumeProfile{TrackID: id, Base: base, Title: title, Stacks: stacks}
	}
	if g.NewRole != nil {
		t := *g.NewRole
		if t.ID == "" {
			t.ID = id
		}
		if err := s.tracks.Upsert(t); err == nil {
			s.log.Info("resume: роль создана по резюме", "id", t.ID, "title", t.Title, "grade", base,
				"stacks", joinStacks(stacks))
			if title == "" {
				title = t.Title
			}
			return resumeProfile{TrackID: t.ID, Base: base, Title: title, Stacks: stacks}
		} else {
			s.log.Warn("resume: сгенерированная роль не провалидировалась — backend", "id", t.ID, "err", err)
		}
	}
	s.log.Info("resume: роль не определена — backend", "track_id", id)
	return resumeProfile{TrackID: models.DefaultTrackID, Base: base, Title: title, Stacks: stacks}
}

// cleanStacks — языки из ответа ИИ в рабочий вид: нижний регистр, только
// поддерживаемые стек-значения (go|python|shell), без повторов. Порядок ИИ
// значим: первый — основной, по нему кодовая стадия.
func cleanStacks(in []models.Stack) []models.Stack {
	out := make([]models.Stack, 0, len(in))
	seen := map[models.Stack]bool{}
	for _, raw := range in {
		s := models.Stack(strings.ToLower(strings.TrimSpace(string(raw))))
		if !s.Valid() || seen[s] {
			continue
		}
		seen[s] = true
		out = append(out, s)
	}
	return out
}

// joinStacks — список языков в одну строку (логи).
func joinStacks(st []models.Stack) string {
	names := make([]string, len(st))
	for i, s := range st {
		names[i] = string(s)
	}
	return strings.Join(names, ",")
}

// targetGrade — цель собеседования: на шаг выше базового грейда по резюме,
// в пределах грейдов роли. Если роль таких грейдов не даёт — её максимальный.
func targetGrade(base models.Grade, allowed []models.Grade) models.Grade {
	if !base.Valid() || len(allowed) == 0 {
		return models.GradeMiddle
	}
	want := models.GradeStep(base, 1)
	maxAllowed := allowed[0]
	for _, g := range allowed {
		if models.GradeRank(g) > models.GradeRank(maxAllowed) {
			maxAllowed = g
		}
	}
	target := maxAllowed
	for _, g := range allowed {
		if models.GradeRank(g) >= models.GradeRank(want) && models.GradeRank(g) < models.GradeRank(target) {
			target = g
		}
	}
	return target
}
