// spec-14/#12 (F2 безопасности): JWT в query-параметре WS попадает в логи
// прокси/сервера (access-log с полным URL). Замена — одноразовый тикет:
// аутентифицированный REST-роут выдаёт токен (TTL 60 с), WS-upgrade его
// consumes (один раз). JWT-путь остаётся (audio-debug.html и совместимость).
package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"net/http"
	"sync"
	"time"
)

// wsTicketEntry — тикет и его владелец.
type wsTicketEntry struct {
	userID  int64
	expires time.Time
}

// wsTicketStore — выдача/списание одноразовых тикетов.
type wsTicketStore struct {
	mu sync.Mutex
	m  map[string]wsTicketEntry
}

func newWSTicketStore() *wsTicketStore {
	return &wsTicketStore{m: make(map[string]wsTicketEntry)}
}

const wsTicketTTL = 60 * time.Second

// issue — новый тикет (24 байта энтропии → 48 hex); заодно чистим просроченные.
func (t *wsTicketStore) issue(userID int64) (string, error) {
	b := make([]byte, 24)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	token := hex.EncodeToString(b)
	t.mu.Lock()
	defer t.mu.Unlock()
	now := time.Now()
	for k, e := range t.m {
		if now.After(e.expires) {
			delete(t.m, k)
		}
	}
	t.m[token] = wsTicketEntry{userID: userID, expires: now.Add(wsTicketTTL)}
	return token, nil
}

// consume — одноразовое использование: токен удаляется сразу (повторное
// подключение с тем же тикетом невозможно).
func (t *wsTicketStore) consume(token string) (int64, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	e, ok := t.m[token]
	if !ok {
		return 0, false
	}
	delete(t.m, token)
	if time.Now().After(e.expires) {
		return 0, false
	}
	return e.userID, true
}

// handleWSTicket — GET /api/v1/ws-ticket (requireAuth): выдаёт одноразовый
// тикет для WS-upgrade вместо JWT в query-параметре (логи прокси не видят
// секрет). TTL 60 с — достаточно на рукопожатие с ретраями клиента.
func (s *Server) handleWSTicket(w http.ResponseWriter, r *http.Request) {
	userID := userIDFromContext(r.Context())
	token, err := s.wsTickets.issue(userID)
	if err != nil {
		s.log.Error("ws-ticket: issue", "err", err)
		writeError(w, http.StatusInternalServerError, "internal", "ошибка выдачи тикета")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ticket": token})
}
