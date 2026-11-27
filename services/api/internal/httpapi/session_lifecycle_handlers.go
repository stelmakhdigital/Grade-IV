package httpapi

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// POST-действия жизненного цикла сессии (pause/resume/finish) и стадия design
// (белая доска, ADR-004/WP-10): всё здесь дёргает session engine напрямую.

// whiteboardMaxBodyBytes — лимит тела PUT /whiteboard (state + base64-картинка).
const whiteboardMaxBodyBytes = 8 << 20

// whiteboardMaxPngBytes — лимит PNG схемы (10 МБ; ADR-004: 1920 px+).
const whiteboardMaxPngBytes = 10 << 20

// handleSessionAction — POST /api/v1/sessions/{id}/{pause|resume|finish}.
func (s *Server) handleSessionAction(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	// spec-14/A1: pause/resume/finish меняют состояние сессии — проверка владения
	// ДО действия (раньше GetOwned вызывался уже ПОСЛЕ действия, т.е. любой
	// аутентифицированный пользователь мог паузить/завершать чужие сессии,
	// включая запуск генерации отчёта и итоговый учёт времени).
	if _, ok := s.ownedSession(w, r, id); !ok {
		return
	}
	var (
		snap *session.Snapshot
		err  error
	)
	switch r.PathValue("action") {
	case "pause":
		if snap, err = s.engine.Pause(id); err == nil {
			// FR-S7 (раунд 2): пауза — session-level стоп TTS (флаг + stop-канал
			// активного стрима + WS tts_stop; гонка «пауза в LLM-фазе» закрыта).
			s.stopTTSOnAction(id, "pause")
		}
	case "resume":
		snap, err = s.engine.Resume(id)
		if err == nil {
			// FR-S7 (раунд 2): возобновление сбрасывает session-level стоп TTS —
			// иначе последующие стримы (приветствие/ходы) умрут мгновенно.
			s.wsSessionFor(id).resetSessionStopped()
		}
	case "finish":
		snap, err = s.engine.Finish(id)
		if err == nil {
			// FR-S7 (раунд 2): завершение во время активного TTS-стрима — стоп
			// аналогично pause (end-кадр + WS tts_stop); лог — отдельно (finish).
			s.stopTTSOnAction(id, "finish")
		}
	default:
		writeError(w, http.StatusNotFound, "not_found", "неизвестное действие")
		return
	}
	if err != nil {
		s.writeSessionEngineError(w, err)
		return
	}
	// Пауза дольше порога: Resume сам прервал сессию (pause_timeout, aborted) —
	// возобновлять нечего, это конфликт (409 invalid_state). Отчёт всё равно
	// генерируем: транскрипт у прерванной сессии есть, а без запуска ReportView
	// поллит «Считаем результаты интервью…» вечно (spec-14, BUG-20261014-021).
	if r.PathValue("action") == "resume" && snap.Status == models.StatusAborted {
		s.startReportGeneration(id)
		writeError(w, http.StatusConflict, "invalid_state", "пауза дольше порога — сессия прервана")
		return
	}
	if r.PathValue("action") == "finish" {
		s.startReportGeneration(id) // WP-11: отчёт после завершения
	}
	m, err := s.sessions.GetOwned(r.Context(), id, userIDFromContext(r.Context()))
	if err != nil {
		// Снимок уже выдан; при ошибке БД отдаём из снимка.
		writeJSON(w, http.StatusOK, sessionDTO{
			ID: id, Stage: string(snap.Stage), Status: string(snap.Status),
			DurationLimitS: snap.DurationLimitS, ActiveSeconds: snap.ActiveSeconds, TimeLeftS: snap.TimeLeftS,
		})
		return
	}
	writeJSON(w, http.StatusOK, toDTO(m, snap))
}

// engineSnapshot — снимок из движка, nil если нет (исторические сессии).
func (s *Server) engineSnapshot(id int64) *session.Snapshot {
	snap, err := s.engine.Snapshot(id)
	if err != nil {
		return nil
	}
	return snap
}

// handleWhiteboardPut — PUT /api/v1/sessions/{id}/whiteboard (ADR-004, WP-10).
// {state (JSON Excalidraw), png? (base64, MVP — не обязателен),
//
//	structure? {blocks: [имена], links: n}} — структура схемы для оценки ИИ.
//
// После сохранения — fire-and-forget ИИ-оценка (ai_text по WS, как code review).
// logDesignCheck — детерминированная сверка схемы с обязательными артефактами
// кейса (spec-24). Отчёт пересчитывает то же самое по событию whiteboard_save;
// отдельное событие нужно для аудита: «чего не хватило» на момент сдачи, а не
// после того, как формулировка кейса или required поменяются в роли.
func (s *Server) logDesignCheck(ctx context.Context, m models.Session, blocks []string) {
	track, err := s.tracks.GetOrDefault(string(m.TrackID))
	if err != nil {
		return
	}
	c := track.Board.CaseForGrade(m.Grade)
	if c == nil || len(c.Required) == 0 {
		return // кейс без обязательных артефактов — сверять нечего
	}
	found, missing := c.Coverage(blocks)
	_, _ = s.sessions.AddEvent(ctx, m.ID, "design_check", mustMarshal(map[string]any{
		"case":     c.Title,
		"required": len(c.Required),
		"found":    found,
		"missing":  missing,
	}))
}

func (s *Server) handleWhiteboardPut(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	if session.IsTerminalStatus(m.Status) {
		writeError(w, http.StatusConflict, "invalid_state", "сессия завершена")
		return
	}

	var body struct {
		State     json.RawMessage `json:"state"`
		Png       string          `json:"png"`
		Structure *struct {
			Blocks []string `json:"blocks"`
			Links  int      `json:"links"`
		} `json:"structure"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, whiteboardMaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request",
			"ожидается JSON {state, png?, structure?}")
		return
	}
	if len(body.State) == 0 || string(body.State) == "null" {
		writeError(w, http.StatusBadRequest, "invalid_request", "state обязателен (JSON Excalidraw)")
		return
	}

	// Структура (детерминированная часть оценки, ADR-004): блоки и связи.
	blocksJSON := []byte("{}")
	structure := map[string]any{"blocks": []string{}, "links": 0}
	if body.Structure != nil {
		structure = map[string]any{"blocks": body.Structure.Blocks, "links": body.Structure.Links}
	}
	blocksJSON, _ = json.Marshal(structure)

	// PNG схемы (base64 → файл на узле; ADR-004: оценка vision по PNG + структуре).
	var pngBytes []byte
	if body.Png != "" {
		decoded, err := base64.StdEncoding.DecodeString(body.Png)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "png: некорректное base64")
			return
		}
		if len(decoded) > whiteboardMaxPngBytes {
			writeError(w, 413, "too_large", "png слишком большой")
			return
		}
		pngBytes = decoded
	}

	if err := s.whiteboards.Save(r.Context(), id, body.State, blocksJSON, pngBytes); err != nil {
		s.log.Error("whiteboard save", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка сохранения холста")
		return
	}
	_, _ = s.sessions.AddEvent(r.Context(), id, "whiteboard_save", mustMarshal(structure))
	blocks, _ := structure["blocks"].([]string)
	s.log.Info("whiteboard сохранён", "session", id, "blocks", len(blocks))
	s.logDesignCheck(r.Context(), m, blocks)

	// ИИ-оценка по рубрике (fire-and-forget; транскрипт устного ответа
	// стадии подтягивает interviewer из событий).
	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), llm.DefaultTimeout)
		defer cancel()
		text, err := s.interviewer.OnDesignSubmit(rctx, id, structure, pngBytes)
		if err != nil {
			s.log.Warn("design: ИИ-оценка не удалась", "session", id, "err", err)
			return
		}
		s.engine.SendTo(id, map[string]any{"type": "ai_text", "text": text})
	}()

	writeJSON(w, http.StatusOK, map[string]any{"saved": true, "structure": structure})
}

// handleWhiteboardGet — GET /api/v1/sessions/{id}/whiteboard (ADR-004): ранее
// сохранённая схема (state Excalidraw + структура) для восстановления холста при
// возврате на стадию design. 200 — схема; сохранений ещё не было — 200 с
// state=null. 409 — сессия завершена.
func (s *Server) handleWhiteboardGet(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	m, ok := s.ownedSession(w, r, id)
	if !ok {
		return
	}
	if session.IsTerminalStatus(m.Status) {
		writeError(w, http.StatusConflict, "invalid_state", "сессия завершена")
		return
	}
	state, blocks, _, err := s.whiteboards.Get(r.Context(), id)
	if err != nil {
		// Схемы ещё нет — штатный случай, а не ошибка запроса: клиент читает
		// холст на каждом входе в стадию, и 404 на первом входе выглядел
		// в консоли как упавший запрос (проверено в браузере).
		if errors.Is(err, db.ErrWhiteboardNotFound) {
			writeJSON(w, http.StatusOK, map[string]any{
				"state": nil, "structure": map[string]any{"blocks": []string{}, "links": 0},
			})
			return
		}
		s.log.Error("whiteboard get", "session", id, "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка чтения холста")
		return
	}
	var structure any
	_ = json.Unmarshal(blocks, &structure)
	if structure == nil {
		structure = map[string]any{"blocks": []string{}, "links": 0}
	}
	writeJSON(w, http.StatusOK, map[string]any{"state": json.RawMessage(state), "structure": structure})
}
