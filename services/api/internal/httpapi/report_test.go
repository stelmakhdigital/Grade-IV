package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
)

// finishSession — финализация + запуск генерации отчёта (как в обработчиках).
func (e *interviewEnv) finishSession(t *testing.T) {
	t.Helper()
	if _, err := e.srv.Engine().Finish(e.session); err != nil {
		t.Fatalf("finish: %v", err)
	}
	e.srv.startReportGeneration(e.session)
}

func getReport(t *testing.T, e *interviewEnv) (int, map[string]any) {
	t.Helper()
	return doJSON(t, "GET",
		fmt.Sprintf("%s/api/v1/sessions/%d/report", e.ts.URL, e.session), nil, authHeader(e.token))
}

// TestReportLifecycle — finish → 202 (генерация) → 200 (отчёт, heuristic по mock).
func TestReportLifecycle(t *testing.T) {
	e := newInterviewEnv(t)
	ctx := context.Background()

	// Материал сессии: реплики кандидата и интервьюера, кодовый запуск (успех), схема.
	if _, err := e.srv.interviewer.OnUserUtterance(ctx, e.session, "Расскажу про структуры данных"); err != nil {
		t.Fatalf("utterance: %v", err)
	}
	_, _ = e.srv.eventData(ctx, e.session, "user_utterance", map[string]any{"text": "Расскажу про структуры данных"})
	_, _ = e.srv.eventData(ctx, e.session, "code_run", map[string]any{"passed": true, "exit_code": 0})
	code, _ := doJSON(t, "PUT",
		fmt.Sprintf("%s/api/v1/sessions/%d/whiteboard", e.ts.URL, e.session),
		map[string]any{"state": json.RawMessage(`{"elements":[]}`),
			"structure": map[string]any{"blocks": []string{"Client", "SQL-БД"}, "links": 1}},
		authHeader(e.token))
	if code != http.StatusOK {
		t.Fatalf("whiteboard: %d", code)
	}

	e.finishSession(t)

	// 200 в разумное время (mock — быстрый) или сначала 202.
	var body map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for {
		code, body = getReport(t, e)
		if code == http.StatusOK {
			break
		}
		if code != http.StatusAccepted {
			t.Fatalf("report: %d %v", code, body)
		}
		if time.Now().After(deadline) {
			t.Fatal("отчёт так и не сгенерирован (202)")
		}
		time.Sleep(100 * time.Millisecond)
	}
	if overall, ok := body["overall"].(float64); !ok || overall <= 0 || overall > 5 {
		t.Fatalf("overall: %v", body["overall"])
	}
	if rec, _ := body["grade_recommendation"].(string); rec == "" {
		t.Fatalf("grade_recommendation пуст: %v", body)
	}
	var crit []map[string]any
	raw, _ := json.Marshal(body["criteria"])
	if err := json.Unmarshal(raw, &crit); err != nil {
		t.Fatalf("criteria: %v (raw %s)", err, raw)
	}
	if len(crit) == 0 {
		t.Fatalf("criteria пуст: %v", body)
	}
	// heuristic: Live-Code 4 (запуск прошёл), коммуникация 3 (была речь).
	for _, c := range crit {
		switch {
		case fmt.Sprint(c["name"]) == "Live-Code (алгоритм, качество кода, тесты)":
			if c["score"] != 4.0 {
				t.Fatalf("livecode score: %v", c)
			}
		case fmt.Sprint(c["name"]) == "Коммуникация (ясность, структура, русский язык)":
			if c["score"] != 3.0 {
				t.Fatalf("comm score: %v", c)
			}
		}
	}
}

// TestReportLLMJSON — управляемый LLM вернул JSON → парсинг (веса из §12).
func TestReportLLMJSON(t *testing.T) {
	e := newInterviewEnv(t)
	jsonResp := `{
		"criteria": [
			{"name": "CS-фундамент (ООП, структуры данных, сложность)", "weight": 0.9, "score": 4, "comment": "хорошо"},
			{"name": "Live-Code (алгоритм, качество кода, тесты)", "weight": 0.9, "score": 5, "comment": "отлично"},
			{"name": "Deep-dive по стеку (Go/Python)", "weight": 0.9, "score": 4, "comment": "глубоко"},
			{"name": "System Design (сервис с нуля, масштабируемость)", "weight": 0.9, "score": 3, "comment": "средне"},
			{"name": "Коммуникация (ясность, структура, русский язык)", "weight": 0.9, "score": 5, "comment": "чётко"}
		],
		"strengths": ["сильная аргументация"],
		"weaknesses": ["мало примеров"],
		"recommendations": ["попрактиковаться в design"]
	}`
	e.mock.SetResponder(func(req llm.Request) (string, error) { return jsonResp, nil })

	e.finishSession(t)
	var body map[string]any
	deadline := time.Now().Add(15 * time.Second)
	for {
		var code int
		code, body = getReport(t, e)
		if code == http.StatusOK {
			break
		}
		if code != http.StatusAccepted || time.Now().After(deadline) {
			t.Fatalf("report: %d %v", code, body)
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Взвешенное среднее по весам §12 (middle): 4*.20+5*.25+4*.25+3*.15+5*.15 = 4.25
	overall, ok := body["overall"].(float64)
	if !ok {
		t.Fatalf("overall: %v", body["overall"])
	}
	if overall < 4.2 || overall > 4.3 {
		t.Fatalf("overall (веса из §12): %v", overall)
	}
	if rec, _ := body["grade_recommendation"].(string); rec != "грейд подтверждён с запасом" {
		t.Fatalf("рекомендация: %q", rec)
	}
}

// TestReportErrors — 409 (активная сессия), 404 (чужая).
func TestReportErrors(t *testing.T) {
	e := newInterviewEnv(t)
	// Активная сессия — 409.
	code, _ := getReport(t, e)
	if code != http.StatusConflict {
		t.Fatalf("активная: %d", code)
	}
	// Чужая сессия — 404.
	var m map[string]any
	code, m = doJSON(t, "POST", e.ts.URL+"/api/v1/auth/register",
		map[string]string{"email": "rep-other@example.com", "password": "password1"}, nil)
	other, _ := m["token"].(string)
	_ = code
	code, _ = doJSON(t, "GET",
		fmt.Sprintf("%s/api/v1/sessions/%d/report", e.ts.URL, e.session), nil, authHeader(other))
	if code != http.StatusNotFound {
		t.Fatalf("чужая: %d", code)
	}
	var _ *db.ReportStore
}
