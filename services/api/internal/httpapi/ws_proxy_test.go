package httpapi

// Регрессии прод-поднятого стенда (Caddy → nginx → api): молчаливый 403 на
// WS-апгрейде, ложный статус в логах, 500 на отсутствующем опциональном конфиге.

import (
	"bufio"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"path/filepath"
	"testing"
)

// TestSessionWSUpgradeBehindProxy — nginx отдаёт api заголовок Host без порта
// (proxy_set_header Host $host), браузер при этом шлёт Origin с портом.
// Встроенная в websocket.Accept проверка same-origin сравнивает строки, не
// совпадающие из-за порта, и отклоняла апгрейд — голосовая стадия не
// начиналась, в логе не было ни строки.
func TestSessionWSUpgradeBehindProxy(t *testing.T) {
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)
	id, _ := createSession(t, ts, token, "middle", "python")

	key := make([]byte, 16)
	if _, err := rand.Read(key); err != nil {
		t.Fatalf("ключ кадра: %v", err)
	}
	req := fmt.Sprintf("GET /ws/session/%d HTTP/1.1\r\n"+
		"Host: grade.internal\r\n"+
		"Authorization: Bearer %s\r\n"+
		"Origin: http://grade.internal:8081\r\n"+
		"Upgrade: websocket\r\nConnection: Upgrade\r\n"+
		"Sec-WebSocket-Key: %s\r\nSec-WebSocket-Version: 13\r\n\r\n",
		id, url.QueryEscape(token), base64.StdEncoding.EncodeToString(key))

	conn, err := net.Dial("tcp", ts.Listener.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer func() { _ = conn.Close() }()
	if _, err := conn.Write([]byte(req)); err != nil {
		t.Fatalf("write: %v", err)
	}
	resp, err := http.ReadResponse(bufio.NewReader(conn), nil)
	if err != nil {
		t.Fatalf("читать ответ апгрейда: %v", err)
	}
	if resp.StatusCode != http.StatusSwitchingProtocols {
		body := make([]byte, 300)
		n, _ := resp.Body.Read(body)
		t.Fatalf("апгрейд за прокси: %d %s", resp.StatusCode, body[:n])
	}
}

// TestStatusRecorderRecordsRealStatus — middleware писал status 200 в каждый
// лог: rec создавался уже со статусом 200, а WriteHeader заполнял поле только
// когда оно было нулевым. 403/404/500 в логах выглядели как 200.
func TestStatusRecorderRecordsRealStatus(t *testing.T) {
	rec := &statusRecorder{ResponseWriter: httptest.NewRecorder()}
	writeError(rec, http.StatusForbidden, "forbidden", "нельзя")
	if rec.status != http.StatusForbidden {
		t.Fatalf("записан статус %d, ожидался 403", rec.status)
	}
}

// TestTTSVoicesWithoutFile — config/tts-voices.json опционален (его создаёт
// admin через PUT). Раньше GET /api/v1/tts-voices на свежем стенде отвечал 500.
func TestTTSVoicesWithoutFile(t *testing.T) {
	t.Setenv("TTS_VOICES_FILE", filepath.Join(t.TempDir(), "нет-такого-файла.json"))
	ts := newTestEnv(t)
	token := registerAndGetToken(t, ts)

	status, body := doJSON(t, "GET", ts.URL+"/api/v1/tts-voices", nil,
		map[string]string{"Authorization": "Bearer " + token})
	if status != http.StatusOK {
		t.Fatalf("реестр голосов без файла: %d %v", status, body)
	}
}
