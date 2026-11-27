// Live-Code: выбор задач из банка sandbox (грейд + stretch, история spec-24),
// вход на стадию, контекст доски (designEntryNote). Отдельный файл: предметная
// логика стадии, а не транспорт WS.

package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"path"
	"strconv"
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// liveCodeTask — задача Live-Code из банка sandbox (ARCHITECTURE.md §4.4).
type liveCodeTask struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Statement string `json:"statement"`
	// Files — файлы, видимые кандидату (solution-шаблон и статические данные
	// задачи). Доверенные файлы градатора (check.sh, *_test.go, test_*.py)
	// не отдаются: spec-20/BUG-20261104-027 — кандидат мог открыть и править
	// градатор (для shell check.sh и есть весь чек). Исполняются ТОЛЬКО
	// файлы банка: sandbox подставляет их по task_id на /runs (доверенность
	// тестов, §4.4).
	Files map[string]string `json:"files,omitempty"`
}

// isGraderFile — файл градатора задачи (правила зеркалят доверенность тестов
// в cmd/sandbox: go *_test.go; python test_*.py|*_test.py; shell check.sh).
// Маски применяются все сразу, без фильтра по стеку (issue #3): стек стадии
// может не совпасть со стеком задачи (старые сессии с пустым stack), и фильтр
// по одному стеку пропускал чужие тесты кандидату (в банке других масок нет).
func isGraderFile(name string) bool {
	base := path.Base(name)
	return strings.HasSuffix(name, "_test.go") || strings.HasSuffix(name, "_test.py") ||
		strings.HasPrefix(base, "test_") || base == "check.sh"
}

// sanitizeTasks — убрать файлы градатора из задач перед отправкой кандидату.
func sanitizeTasks(tasks []liveCodeTask) []liveCodeTask {
	out := make([]liveCodeTask, 0, len(tasks))
	for _, t := range tasks {
		files := make(map[string]string, len(t.Files))
		for name, content := range t.Files {
			if isGraderFile(name) {
				continue
			}
			files[name] = content
		}
		if len(files) == 0 {
			files = nil
		}
		t.Files = files
		out = append(out, t)
	}
	return out
}

// liveCodeTaskCount — число задач на стадию Live-Code по грейду (spec-13B T1):
// junior=2, middle=3, senior=3, staff=3. Таблица в одном месте.
var liveCodeTaskCount = map[models.Grade]int{
	models.GradeJunior: 2,
	models.GradeMiddle: 3,
	models.GradeSenior: 3,
	models.GradeStaff:  3,
}

// liveCodeStretchGrade — для middle/senior последняя задача — из пула следующего
// грейда (middle→senior, senior→staff); junior/staff — без stretch (spec-13B T1.2).
func liveCodeStretchGrade(g models.Grade) (models.Grade, bool) {
	switch g {
	case models.GradeMiddle:
		return models.GradeSenior, true
	case models.GradeSenior:
		return models.GradeStaff, true
	}
	return g, false
}

// liveCodeSeed — LIVECODE_TASK_SEED (env, целое): детерминированный выбор для
// тестов/E2E; не задан (или не целое) — рандом.
func liveCodeSeed() (int64, bool) {
	v := os.Getenv("LIVECODE_TASK_SEED")
	if v == "" {
		return 0, false
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		return 0, false
	}
	return n, true
}

// pickLiveCodeTasks — N различных случайных задач из пула грейда; для
// middle/senior последняя — из пула следующего грейда (stretch, если непустой).
//
// seen — задачи, которые этот кандидат уже получал (spec-24: пул не должен
// повторяться от интервью к интервью). Свежих не хватило — добираем из виденных
// и возвращаем reused=true: лучше повтор, чем кандидат получит одну задачу вместо
// трёх.
func pickLiveCodeTasks(grade models.Grade, pool, stretchPool []liveCodeTask, seen map[string]bool, rng *rand.Rand) ([]liveCodeTask, bool) {
	fresh, stale := splitUnseen(pool, seen)
	freshStretch, staleStretch := splitUnseen(stretchPool, seen)
	out := pickTasksFrom(grade, fresh, freshStretch, rng)

	want := liveCodeTaskCount[grade]
	if want <= 0 {
		want = 1
	}
	if len(out) >= want {
		return out, false
	}
	picked := make(map[string]bool, len(out))
	for _, t := range out {
		picked[t.ID] = true
	}
	topup := make([]liveCodeTask, 0, len(stale)+len(staleStretch))
	for _, t := range append(append([]liveCodeTask(nil), stale...), staleStretch...) {
		if !picked[t.ID] {
			topup = append(topup, t)
		}
	}
	rng.Shuffle(len(topup), func(i, j int) { topup[i], topup[j] = topup[j], topup[i] })
	reused := false
	for _, t := range topup {
		if len(out) >= want {
			break
		}
		out = append(out, t)
		picked[t.ID] = true
		reused = true
	}
	return out, reused
}

// splitUnseen — свежие (не из истории кандидата) и виденные ранее задачи.
func splitUnseen(pool []liveCodeTask, seen map[string]bool) (fresh, stale []liveCodeTask) {
	for _, t := range pool {
		if seen[t.ID] {
			stale = append(stale, t)
		} else {
			fresh = append(fresh, t)
		}
	}
	return fresh, stale
}

// pickTasksFrom — N различных случайных (uniform, без повторов) задач из пула
// грейда; последняя — из stretch-пула, если он непустой. Пул меньше N — взять
// всё (грациозная деградация).
func pickTasksFrom(grade models.Grade, pool, stretchPool []liveCodeTask, rng *rand.Rand) []liveCodeTask {
	n := liveCodeTaskCount[grade]
	if n <= 0 {
		n = 1
	}
	out := make([]liveCodeTask, 0, n)
	if len(pool) > 0 {
		main := n
		if len(stretchPool) > 0 {
			main = n - 1 // последняя — stretch
			if main < 0 {
				main = 0
			}
		}
		if main > len(pool) {
			main = len(pool)
		}
		perm := rng.Perm(len(pool))
		for i := 0; i < main; i++ {
			out = append(out, pool[perm[i]])
		}
	}
	if len(stretchPool) > 0 {
		out = append(out, stretchPool[rng.Intn(len(stretchPool))])
	}
	if len(out) == 0 && len(pool) > 0 {
		out = append(out, pool[rng.Intn(len(pool))])
	}
	return out
}

// resendLiveCodeTasks — повтор назначенных задач при подключении к практике.
// ResendStage отдаёт стадию без задач (их выбирает enterLiveCode), и после
// перезагрузки страницы кандидат оставался с пустым редактором, без условия
// задачи и без вкладок (замерено в браузере). Задачи уже лежат в событии
// livecode_tasks — берём оттуда, нового состояния нет. `track` не шлём:
// клиент сохраняет мету из предыдущего stage-сообщения (useVoiceSession:212).
func (s *Server) resendLiveCodeTasks(ctx context.Context, id int64) {
	events, err := s.sessions.ListEventsTail(ctx, id, 200)
	if err != nil {
		s.log.Warn("livecode: не удалось прочитать события для повтора задач", "session", id, "err", err)
		return
	}
	for i := len(events) - 1; i >= 0; i-- {
		if events[i].Kind != "livecode_tasks" {
			continue
		}
		var payload struct {
			Tasks []liveCodeTask `json:"tasks"`
		}
		if err := json.Unmarshal(events[i].Data, &payload); err != nil || len(payload.Tasks) == 0 {
			return // битое/пустое событие — повторяять нечего
		}
		s.engine.SendTo(id, map[string]any{
			"type":  "stage",
			"name":  models.StageLiveCode,
			"tasks": payload.Tasks,
			"task":  payload.Tasks[0], // совместимость: task = tasks[0]
		})
		return
	}
}

// enterLiveCode — вход на стадию Live-Code (WP-5/6, spec-13B T1): случайный набор
// задач из банка (N по грейду + stretch), сообщение stage {tasks, task},
// событие livecode_tasks (для отчёта), лог выбранных id, ИИ-комментарий входа.
func (s *Server) enterLiveCode(ctx context.Context, id int64, ws *wsSession) {
	sess, err := s.sessions.Get(ctx, id)
	if err != nil {
		return
	}
	// Один источник стека для обоих пулов (issue #3): stack сессии может быть пуст
	// (старые строки БД, роль, которой нет в файле ролей). Запрос в банк без
	// фильтра по стеку вернул бы задачи всех стеков — при неизвестном стеке
	// стадии задачи не берём вовсе (кандидат получит «задачи ещё не назначены»).
	stageStack := s.liveCodeStack(sess)
	var tasks []liveCodeTask
	// reused — набор пришлось добирать уже виденными задачами (см. pickLiveCodeTasks).
	reused := false
	if stageStack == "" {
		s.log.Warn("livecode: стек стадии неизвестен — задачи не выбраны", "session", id)
	} else if pool, err := s.fetchLiveCodeTasks(ctx, stageStack, string(sess.Grade)); err != nil {
		s.log.Warn("livecode: банк задач недоступен", "session", id, "err", err)
	} else {
		var stretchPool []liveCodeTask
		if nextGrade, ok := liveCodeStretchGrade(sess.Grade); ok {
			if sp, err := s.fetchLiveCodeTasks(ctx, stageStack, string(nextGrade)); err == nil && len(sp) > 0 {
				stretchPool = sp
			}
		}
		seed, seeded := liveCodeSeed()
		rng := rand.New(rand.NewSource(time.Now().UnixNano()))
		if seeded {
			rng = rand.New(rand.NewSource(seed))
		}
		tasks, reused = pickLiveCodeTasks(sess.Grade, pool, stretchPool, s.seenLiveCodeIDs(ctx, sess), rng)
		if reused {
			s.log.Info("livecode: свежих задач не хватило — добавлены уже виденные",
				"session", id, "grade", sess.Grade)
		}
		if len(tasks) > 0 {
			ids := make([]string, 0, len(tasks))
			for _, t := range tasks {
				ids = append(ids, t.ID)
			}
			s.log.Info("livecode: выбранные задачи", "session", id, "tasks", ids, "seeded", seeded)
			// spec-20/BUG-20261104-027: файлы градатора кандидату не отдаются
			// (в транскрипте событие тоже хранится уже без них).
			tasks = sanitizeTasks(tasks)
			// Событие livecode_tasks — для per-task отчёта (spec-13B T3).
			_, _ = s.eventData(ctx, id, "livecode_tasks", map[string]any{"tasks": tasks})
			// spec-20: track-мета в сообщении с задачами — иначе оно затирает
			// trackMeta клиента (m.track ?? null) и nav по стадиям ломается
			// (BUG-20261104-028: второй stage-livecode без track сбрасывал UI).
			msg := map[string]any{
				"type": "stage", "name": models.StageLiveCode,
				"tasks": tasks,
				"task":  tasks[0], // совместимость: task = tasks[0]
			}
			if s.tracks != nil && sess.TrackID != "" {
				if tr, err := s.tracks.Get(sess.TrackID); err == nil && tr != nil {
					// issue #7: навигация — по программе стадий, закреплённой за
					// сессией, а не по текущей версии роли.
					if len(sess.Stages) > 0 {
						msg["track"] = tr.StageMetaFor(sess.Stages, sess.Stack)
					} else {
						msg["track"] = tr.StageMeta(sess.Grade)
					}
				}
			}
			s.engine.SendTo(id, msg)
		}
	}
	text, err := s.interviewer.OnStageChanged(ctx, id, stageNote(tasks))
	s.speak(ctx, id, ws, text, err) // озвучка задачи (TTS)
}

// seenLiveCodeIDs — задачи, которые этот кандидат уже получал на Live-Code
// (spec-24). Смотрим события livecode_tasks последних его сессий; ошибка БД не
// блокирует стадию — задачи возьмём без учёта истории.
func (s *Server) seenLiveCodeIDs(ctx context.Context, sess models.Session) map[string]bool {
	if sess.UserID == 0 {
		return nil
	}
	rows, err := s.sessions.ListRecentEventDataByKindForUser(ctx, sess.UserID, "livecode_tasks", sess.ID, 64)
	if err != nil {
		s.log.Warn("livecode: история задач недоступна", "session", sess.ID, "err", err)
		return nil
	}
	seen := make(map[string]bool)
	for _, raw := range rows {
		var ev struct {
			Tasks []struct {
				ID string `json:"id"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(raw, &ev); err != nil {
			continue
		}
		for _, t := range ev.Tasks {
			if t.ID != "" {
				seen[t.ID] = true
			}
		}
	}
	return seen
}

// liveCodeStack — стек практической стадии (spec-19): у кодовых треков — стек
// сессии; у shell-треков сисадмина stack сессии пуст, стек задач берём из
// банка shell (tasks?stack=shell). Не-shell некодовые задачи не дойдут сюда:
// стадия livecode не входит в их программу.
func (s *Server) liveCodeStack(sess models.Session) string {
	if sess.Stack != "" {
		return string(sess.Stack)
	}
	if s.tracks != nil && sess.TrackID != "" {
		if t, err := s.tracks.Get(sess.TrackID); err == nil && t != nil {
			return string(t.Runtime())
		}
	}
	return ""
}

// stageNote — контекст входа на Live-Code для LLM (список задач, если выбраны):
// первая — основная, остальные — stretch/bonus на усмотрение кандидата.
func stageNote(tasks []liveCodeTask) string {
	if len(tasks) == 0 {
		return "Задачи ещё не назначены (банк задач недоступен). Спроси, готов ли кандидат, и напомни формат стадии."
	}
	b := strings.Builder{}
	fmt.Fprintf(&b, "Сессии назначено %d задач: первая — основная, остальные — stretch/bonus на усмотрение кандидата. Озвучи список при входе.\n", len(tasks))
	for i, t := range tasks {
		role := "основная"
		if i > 0 {
			role = "stretch/bonus"
		}
		fmt.Fprintf(&b, "Задача %d (%s): «%s» (id=%s). Условие: %s\n", i+1, role, t.Title, t.ID, t.Statement)
	}
	return b.String()
}

// fetchLiveCodeTasks — GET {SANDBOX_URL}/api/v1/tasks?stack=&grade= (5 с).
func (s *Server) fetchLiveCodeTasks(ctx context.Context, stack, grade string) ([]liveCodeTask, error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet,
		s.cfg.SandboxURL+"/api/v1/tasks?stack="+url.QueryEscape(stack)+"&grade="+url.QueryEscape(grade), nil)
	if err != nil {
		return nil, err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("sandbox /tasks: HTTP %d", resp.StatusCode)
	}
	var tasks []liveCodeTask
	if err := json.NewDecoder(io.LimitReader(resp.Body, 1<<20)).Decode(&tasks); err != nil {
		return nil, err
	}
	return tasks, nil
}

// hasAIUtterance — в событиях сессии есть ai_utterance (ИИ уже приветствовал/отвечал).
// spec-14/A6: EXISTS-запрос вместо чтения всех событий (ws-подключение на
// каждое подключение клиента).
func hasAIUtterance(ctx context.Context, sessions *db.SessionStore, id int64) bool {
	ok, err := sessions.HasEventKind(ctx, id, "ai_utterance")
	if err != nil {
		return true // при ошибке не спамим приветствием
	}
	return ok
}

// designEntryNote — контекст входа на стадию доски для LLM (spec-18): трек
// (заголовок + кейс грейда) или legacy-текст для backend-хардкода.
func (s *Server) designEntryNote(sessionID int64) string {
	snap, err := s.engine.Snapshot(sessionID)
	if err != nil || snap == nil {
		return "Кандидат переходит к System Design. Представь формат стадии и задай первую задачу на проектирование."
	}
	t, terr := s.tracks.Get(snap.TrackID)
	if terr != nil || t == nil {
		return "Кандидат переходит к System Design. Представь формат стадии и задай первую задачу на проектирование."
	}
	note := "Кандидат переходит к стадии доски «" + t.Board.Title + "». Представь формат стадии"
	if c := t.Board.CaseForGrade(snap.Grade); c != nil {
		note += " и объяви кейс: «" + c.Title + "». " + c.Statement
	} else {
		note += " и задай первую задачу."
	}
	note += " Попроси собирать схему на доске и объяснять решения вслух."
	return note
}
