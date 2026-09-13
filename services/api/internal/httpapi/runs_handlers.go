package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/interviewer"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// Прокси-лимиты: sandbox обещает ≤10 с (ADR-003) — берём с запасом.
const (
	runsProxyTimeout = 15 * time.Second
	runsMaxBodyBytes = 1 << 20 // 1 МБ исходников
)

// runProxyRequest — тело запроса к sandbox (ARCHITECTURE.md §4.4).
type runProxyRequest struct {
	Stack  string            `json:"stack"`
	Action string            `json:"action"`
	Files  map[string]string `json:"files"`
	TaskID string            `json:"task_id,omitempty"`
}

// runProxyResult — результат запуска (контракт sandbox §4.4).
type runProxyResult struct {
	ExitCode   int    `json:"exit_code"`
	Stdout     string `json:"stdout"`
	Stderr     string `json:"stderr"`
	DurationMS int    `json:"duration_ms"`
	Passed     bool   `json:"passed"`
	Timeout    bool   `json:"timeout"`
	Tests      []struct {
		Name   string `json:"name"`
		Passed bool   `json:"passed"`
	} `json:"tests"`
}

// handleSessionRuns — POST /api/v1/sessions/{id}/runs (ARCHITECTURE.md §4.1/§4.4).
// Прокси в sandbox + сохранение в submissions + событие code_run + run_result по WS.
func (s *Server) handleSessionRuns(w http.ResponseWriter, r *http.Request) {
	id, ok := parseSessionID(w, r)
	if !ok {
		return
	}
	userID := userIDFromContext(r.Context())
	m, err := s.sessions.GetOwned(r.Context(), id, userID)
	if errors.Is(err, db.ErrSessionNotFound) {
		writeError(w, http.StatusNotFound, "not_found", "сессия не найдена")
		return
	}
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "не удалось получить сессию")
		return
	}
	// Live-Code — стадия кода; завершённые сессии сдавать не могут.
	if session.IsTerminalStatus(m.Status) {
		writeError(w, http.StatusConflict, "invalid_state", "сессия завершена")
		return
	}
	if m.Stage != models.StageLiveCode {
		writeError(w, http.StatusConflict, "invalid_state",
			"сдача кода доступна только на стадии Live-Code (текущая: "+string(m.Stage)+")")
		return
	}

	var body struct {
		Files  map[string]string `json:"files"`
		Action string            `json:"action"`
		TaskID string            `json:"task_id"`
	}
	r.Body = http.MaxBytesReader(w, r.Body, runsMaxBodyBytes)
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "ожидается JSON {files, action, task_id}")
		return
	}
	if len(body.Files) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "files не могут быть пустыми")
		return
	}
	if body.Action == "" {
		body.Action = "test"
	}

	// Прокси в sandbox.
	proxyCtx, cancel := context.WithTimeout(r.Context(), runsProxyTimeout)
	defer cancel()
	reqBody, err := json.Marshal(runProxyRequest{
		Stack: string(m.Stack), Action: body.Action, Files: body.Files, TaskID: body.TaskID,
	})
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "сериализация запроса к sandbox")
		return
	}
	reqURL := s.cfg.SandboxURL + "/api/v1/sessions/" + fmt.Sprint(id) + "/runs"
	req, err := http.NewRequestWithContext(proxyCtx, http.MethodPost, reqURL, bytes.NewReader(reqBody))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "internal", "запрос к sandbox: "+err.Error())
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		s.log.Error("sandbox: запрос не выполнен", "session", id, "err", err)
		writeError(w, http.StatusServiceUnavailable, "sandbox_unavailable", "sandbox-сервис недоступен")
		return
	}
	defer resp.Body.Close()
	respRaw, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20))
	if err != nil {
		writeError(w, http.StatusBadGateway, "sandbox_error", "ошибка чтения ответа sandbox")
		return
	}
	if resp.StatusCode != http.StatusOK {
		var e struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		}
		_ = json.Unmarshal(respRaw, &e)
		s.log.Warn("sandbox: ошибка запуска", "session", id, "status", resp.StatusCode, "code", e.Code)
		status := http.StatusBadGateway
		if resp.StatusCode == http.StatusServiceUnavailable {
			status = http.StatusServiceUnavailable
		}
		writeError(w, status, "sandbox_error", e.Message)
		return
	}
	var result runProxyResult
	if err := json.Unmarshal(respRaw, &result); err != nil {
		s.log.Error("sandbox: некорректный ответ", "session", id, "err", err)
		writeError(w, http.StatusBadGateway, "sandbox_error", "некорректный ответ sandbox")
		return
	}

	// Сохраняем сдачу и фиксируем событие (история, FR-A4; отчёт — WP-11).
	exit := result.ExitCode
	dur := result.DurationMS
	subm := models.Submission{
		SessionID:  id,
		TaskID:     body.TaskID,
		Files:      mustMarshal(body.Files),
		Action:     body.Action,
		ExitCode:   &exit,
		Stdout:     result.Stdout,
		Stderr:     result.Stderr,
		DurationMS: &dur,
		Tests:      mustMarshal(result.Tests),
	}
	if _, err := s.submissions.Save(r.Context(), subm); err != nil {
		s.log.Error("submissions: сохранение", "session", id, "err", err)
	}
	_, _ = s.sessions.AddEvent(r.Context(), id, "code_run", mustMarshal(map[string]any{
		"task_id": body.TaskID, "passed": result.Passed,
		"exit_code": result.ExitCode, "duration_ms": result.DurationMS,
	}))

	// run_result — на WS (UI Live-Code, ARCHITECTURE.md §4.2).
	s.engine.SendTo(id, map[string]any{
		"type":      "run_result",
		"exit_code": result.ExitCode, "stdout": result.Stdout, "stderr": result.Stderr,
		"duration_ms": result.DurationMS, "passed": result.Passed, "tests": result.Tests,
	})

	// ИИ-ревью по результатам (WP-5): fire-and-forget, ai_text по WS (REST не ждём).
	failed := 0
	for _, t := range result.Tests {
		if !t.Passed {
			failed++
		}
	}
	go func() {
		rctx, cancel := context.WithTimeout(context.Background(), llm.DefaultTimeout)
		defer cancel()
		text, err := s.interviewer.OnCodeRun(rctx, id, map[string]any{
			"task_id": body.TaskID, "passed": result.Passed, "exit_code": result.ExitCode,
			"duration_ms": result.DurationMS, "tests_total": len(result.Tests), "tests_failed": failed,
			"hint": interviewer.CodeRunHint(result.Passed, result.ExitCode, result.DurationMS, len(result.Tests), failed),
		})
		if err != nil {
			s.log.Warn("runs: ИИ-ревью не удалось", "session", id, "err", err)
			return
		}
		s.engine.SendTo(id, map[string]any{"type": "ai_text", "text": text})
	}()

	writeJSON(w, http.StatusOK, result)
}

func mustMarshal(v any) []byte {
	b, err := json.Marshal(v)
	if err != nil {
		return []byte("{}")
	}
	return b
}
