package httpapi

import (
	"errors"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/db"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// TestClientErrorHidesInternals — в WS-кадре error и в HTTP-ответах раньше
// ехал err.Error(): кандидат мог увидеть «Post "http://172.17.0.1:8111/tts":
// dial tcp: connection refused» — внутренние адреса, порты и родной язык
// инфраструктуры. Наружу идут код и нейтральное сообщение, подробности — в лог.
func TestClientErrorHidesInternals(t *testing.T) {
	inner := errors.New(`voice tts: Post "http://172.17.0.1:8111/api/v1/tts": dial tcp: connect: connection refused`)
	code, msg := clientError(inner)
	if code != "internal" || msg != "внутренняя ошибка" {
		t.Fatalf("неизвестная ошибка наружу: %s / %q", code, msg)
	}
	for _, leak := range []string{"172.17", "dial tcp", "connection refused", "tts"} {
		if strings.Contains(msg, leak) {
			t.Fatalf("в сообщение клиенту течёт внутреннее (%q): %q", leak, msg)
		}
	}

	// Наши sentinel-тексты остаются: они человекочитаемые и ничего не выдают.
	if code, msg = clientError(session.ErrIllegalStage); code != "invalid_state" ||
		msg != session.ErrIllegalStage.Error() {
		t.Fatalf("ErrIllegalStage: %s / %q", code, msg)
	}
	if code, msg = clientError(db.ErrSessionNotFound); code != "not_found" || msg != "сессия не найдена" {
		t.Fatalf("ErrSessionNotFound: %s / %q", code, msg)
	}
	if code, _ = clientError(session.ErrSessionEnded); code != "session_ended" {
		t.Fatalf("ErrSessionEnded: код %s, want session_ended", code)
	}
}

// TestWriteSessionEngineErrorStatuses — коды HTTP не поехали: 404 для несуществующей
// сессии, 409 для неверного состояния, 500 (с логом) для всего остального.
func TestWriteSessionEngineErrorStatuses(t *testing.T) {
	cases := []struct {
		err  error
		want int
		code string
	}{
		{db.ErrSessionNotFound, 404, "not_found"},
		{session.ErrIllegalStatus, 409, "invalid_state"},
		{session.ErrSessionEnded, 409, "session_ended"},
		{errors.New("boom"), 500, "internal"},
	}
	for _, tc := range cases {
		rec := httptest.NewRecorder()
		s := &Server{log: discardLogger()}
		s.writeSessionEngineError(rec, tc.err)
		if rec.Code != tc.want {
			t.Fatalf("%v: HTTP %d, want %d", tc.err, rec.Code, tc.want)
		}
	}
}
