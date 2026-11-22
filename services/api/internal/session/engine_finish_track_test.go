package session

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"nhooyr.io/websocket"
)

// attachTestWS — настоящее WS-соединение к движку (httptest + nhooyr), чтобы
// ловить то, что реально уходит клиенту в sendJSON. Возвращает клиентскую
// сторону: сообщения читаются через readWSJSON.
func attachTestWS(t *testing.T, e *Engine, id, userID int64) *websocket.Conn {
	t.Helper()
	handlerDone := make(chan struct{})
	accepted := make(chan *websocket.Conn, 1)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		c, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
		if err != nil {
			t.Errorf("accept: %v", err)
			return
		}
		accepted <- c
		// nhooyr живёт, пока хендлер не вернулся: держим до конца теста.
		<-handlerDone
	}))
	t.Cleanup(func() { close(handlerDone); srv.Close() })

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cli, _, err := websocket.Dial(ctx, "ws"+strings.TrimPrefix(srv.URL, "http"), nil)
	if err != nil {
		t.Fatalf("ws dial: %v", err)
	}
	t.Cleanup(func() { cli.CloseNow() })

	srvConn := <-accepted
	if _, err := e.Attach(ctx, id, userID, srvConn); err != nil {
		t.Fatalf("attach: %v", err)
	}
	return cli
}

// readWSJSON — следующее текстовое сообщение клиента, распарсенное (timer и
// прочие служебные пропускаются на стороне вызывающего теста).
func readWSJSON(t *testing.T, cli *websocket.Conn) map[string]any {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, raw, err := cli.Read(ctx)
	if err != nil {
		t.Fatalf("ws read: %v", err)
	}
	var m map[string]any
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatalf("ws json: %v (%s)", err, raw)
	}
	return m
}

// waitStage — дождаться stage-сообщения о стадии want (таймер/служебные не мешают).
func waitStage(t *testing.T, cli *websocket.Conn, want models.Stage) map[string]any {
	t.Helper()
	for i := 0; i < 20; i++ {
		m := readWSJSON(t, cli)
		if m["type"] == "stage" && m["name"] == string(want) {
			return m
		}
	}
	t.Fatalf("stage/%s не дождались", want)
	return nil
}

// sysadminTrack — роль с shell-практикой: её мета и проверяем в сообщениях.
func sysadminTrack() models.Track {
	return models.Track{
		ID: "sysadmin", Title: "Системный администратор",
		Practice: &models.Practice{Runtime: models.StackShell},
		Grades:   []models.Grade{models.GradeJunior, models.GradeMiddle},
	}
}

// TestFinishStageMessageCarriesTrack (issue #10): о переходе на report
// сообщает finish — он не добавлял track-мету, а UI ставит её безусловно
// (setTrackMeta(m.track ?? null)). Итог на финале: шапка «Интервью: middle»
// вместо имени роли, отчёт подписывает shell-практику как «Live-Code».
// Transition(stage_action: report) делегирует сообщение в finish, поэтому
// проверяем finish — им покрыты оба пути.
func TestFinishStageMessageCarriesTrack(t *testing.T) {
	conn, dialect, err := db.Open("sqlite://:memory:")
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	ctx := context.Background()
	if err := db.Migrate(ctx, conn, dialect); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	e := newTestEnvWithDB(t, conn, dialect)
	sysadmin := sysadminTrack()
	e.engine.tracks = func(id string) (*models.Track, error) {
		if id == sysadmin.ID {
			return &sysadmin, nil
		}
		return nil, nil
	}
	m, err := e.engine.Create(ctx, e.userID, models.GradeMiddle, models.StackGo,
		WithTrack(sysadmin.ID))
	if err != nil {
		t.Fatalf("create: %v", err)
	}

	cli := attachTestWS(t, e.engine, m.ID, e.userID)
	if _, err := e.engine.Finish(m.ID); err != nil {
		t.Fatalf("finish: %v", err)
	}

	msg := waitStage(t, cli, models.StageReport)
	tm, ok := msg["track"].(map[string]any)
	if !ok {
		t.Fatalf("stage(report) без track-меты: %v", msg)
	}
	if tm["title"] != "Системный администратор" {
		t.Fatalf("title: %v", tm["title"])
	}
	pr, ok := tm["practice"].(map[string]any)
	if !ok || pr["runtime"] != string(models.StackShell) {
		t.Fatalf("practice.runtime (подпись «Практика (bash)»): %v", tm["practice"])
	}
}
