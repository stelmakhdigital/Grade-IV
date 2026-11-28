package httpapi

import (
	"net/http"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// Подбор роли по резюме должен быть виден кандидату сразу после сохранения:
// раньше резюме сохранялось молча, и какой грейд поставят интервью — неизвестно
// до старта (ui-review 2026-11-28).
func TestResumeGuessVisible(t *testing.T) {
	const answer = `{"track_id":"backend","grade":"senior",` +
		`"reason":"6 лет Go и распределённые системы","title":"Go-разработчик",` +
		`"stacks":["go"],"new_track":null}`
	ts, tok := newResumeEnv(t, answer)
	auth := map[string]string{"Authorization": "Bearer " + tok}

	code, out := putResume(t, ts.URL+"/api/v1/me/resume", tok, "Go 6 лет, микросервисы", "", nil)
	if code != http.StatusOK {
		t.Fatalf("PUT: код %d, ответ %v", code, out)
	}
	g, _ := out["guess"].(map[string]any)
	if g == nil {
		t.Fatalf("в ответе PUT нет подбора роли: %v", out)
	}
	if g["track_id"] != "backend" || g["grade"] != "senior" ||
		g["title"] != "Go-разработчик" || g["stacks"] != "go" {
		t.Errorf("подбор не совпал с ответом ИИ: %v", g)
	}
	if g["reason"] == "" || g["reason"] != "6 лет Go и распределённые системы" {
		t.Errorf("причина выбора не передана: %v", g["reason"])
	}
	// Цель интервью — не ниже базы (обычно на шаг выше, в пределах грейдов роли).
	target, _ := g["target"].(string)
	if models.GradeRank(models.Grade(target)) < models.GradeRank(models.GradeSenior) {
		t.Errorf("цель %q ниже базы %v", target, g["grade"])
	}

	// Тот же подбор отдаётся при чтении (перезагрузка кабинета его не теряет).
	code, got := doJSON(t, http.MethodGet, ts.URL+"/api/v1/me/resume", nil, auth)
	if code != http.StatusOK {
		t.Fatalf("GET: код %d", code)
	}
	g2, _ := got["guess"].(map[string]any)
	if g2 == nil || g2["title"] != "Go-разработчик" {
		t.Fatalf("подбор не вернулся из GET: %v", got)
	}

	// Сброшено резюме — сброшен и подбор (иначе кабинет врал бы про роль).
	if code, _ := doJSON(t, http.MethodDelete, ts.URL+"/api/v1/me/resume", nil, auth); code != http.StatusOK {
		t.Fatalf("DELETE: код %d", code)
	}
	_, got = doJSON(t, http.MethodGet, ts.URL+"/api/v1/me/resume", nil, auth)
	if got["guess"] != nil {
		t.Errorf("после сброса подбор остался: %v", got["guess"])
	}
}

// ИИ не ответил (или ответил чем попало) — подбор не выдумываем: поля нет,
// кабинет скажет «подберём при старте», а старт интервью посчитает сам.
func TestResumeGuessAbsentWhenLLMSilent(t *testing.T) {
	ts, tok := newResumeEnv(t, "") // резетор не задан → mock отвечает "ok", JSON не разбирается

	code, out := putResume(t, ts.URL+"/api/v1/me/resume", tok, "Go 6 лет", "", nil)
	if code != http.StatusOK {
		t.Fatalf("PUT: код %d, ответ %v", code, out)
	}
	if out["guess"] != nil {
		t.Errorf("подбор появился без внятного ответа ИИ: %v", out["guess"])
	}
}
