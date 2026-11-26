package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// Проверено на живом стенде: 8 неверных паролей подряд давали 401 без единой
// задержки, а верный пароль сразу после них — 200. Каждый неверный при этом
// стоит bcrypt ~60 мс CPU, то есть сам эндпоинт входа был и каналом перебора,
// и способом положить api.

func TestRateLimiterWindow(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(3, time.Minute)
	l.now = func() time.Time { return now }

	for i := 0; i < 3; i++ {
		if ok, _ := l.allow("a"); !ok {
			t.Fatalf("попытка %d: отказала в пустом окне", i+1)
		}
	}
	ok, retry := l.allow("a")
	if ok {
		t.Fatal("четвёртая попытка прошла — лимит не держит")
	}
	if retry < 1 || retry > 60 {
		t.Errorf("Retry-After = %d, хочу 1..60", retry)
	}
	// Другой ключ не заражается.
	if ok, _ := l.allow("b"); !ok {
		t.Error("ключ b получил отказ из-за ключа a")
	}
	// Окно прошло — снова можно.
	now = now.Add(time.Minute + time.Second)
	if ok, _ := l.allow("a"); !ok {
		t.Error("после окна лимит не отпустил")
	}
}

// Карта лимитера не должна расти бесконечно: ключи, которые больше не
// появляются (ушёл кандидат, сменился IP), обязаны вычищаться.
func TestRateLimiterSweepsDeadKeys(t *testing.T) {
	now := time.Now()
	l := newRateLimiter(2, time.Minute)
	l.now = func() time.Time { return now }
	for i := 0; i < sweepEvery+50; i++ {
		l.allow(fmt.Sprintf("k%d", i))
	}
	before := len(l.hits)
	now = now.Add(2 * time.Minute)
	l.allow("живой")
	if len(l.hits) >= before {
		t.Fatalf("ключей было %d, стало %d — мёртвые не вычищаются", before, len(l.hits))
	}
}

// Ключ логина читает тело запроса, чтобы достать email. Если тело не вернуть
// на место, обработчик получит пустой запрос и вход умрёт — это ровно та
// ошибка, которую легко внести и невозможно заметить до прода.
func TestLoginEmailKeyKeepsBodyReadable(t *testing.T) {
	body := `{"email":"Кандидат@Example.com ","password":"x"}`
	r, err := http.NewRequest("POST", "/api/v1/auth/login", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	if got := loginEmailKey(r); got != "login:кандидат@example.com" {
		t.Fatalf("ключ = %q", got)
	}
	rest, err := io.ReadAll(r.Body)
	if err != nil || string(rest) != body {
		t.Fatalf("тело после ключа: %q, err=%v", rest, err)
	}
	var back map[string]string
	if err := json.Unmarshal(rest, &back); err != nil || back["password"] != "x" {
		t.Fatalf("тело не разбирается повторно: %v", err)
	}
}

// Сквозная проверка: восьмой неверный вход в аккаунт получает 429, верный пароль
// до лимита работает, а другой аккаунт не блокируется первым.
func TestLoginThrottle(t *testing.T) {
	e := newInterviewEnv(t)
	loginURL := fmt.Sprintf("%s/api/v1/auth/login", e.ts.URL)
	post := func(email, pw string) (int, string) {
		raw, _ := json.Marshal(map[string]string{"email": email, "password": pw})
		resp, err := http.Post(loginURL, "application/json", bytes.NewReader(raw))
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		_, _ = io.Copy(io.Discard, resp.Body)
		return resp.StatusCode, resp.Header.Get("Retry-After")
	}

	for i := 0; i < 8; i++ {
		if code, _ := post("check@example.com", "не-пароль"); code != http.StatusUnauthorized {
			t.Fatalf("попытка %d: %d, хочу 401", i+1, code)
		}
	}
	code, retry := post("check@example.com", "не-пароль")
	if code != http.StatusTooManyRequests {
		t.Fatalf("девятая попытка: %d, хочу 429", code)
	}
	if n, err := strconv.Atoi(retry); err != nil || n < 1 {
		t.Errorf("Retry-After = %q", retry)
	}
	// Другой аккаунт лимит не трогает.
	reg := registerAndGetToken(t, e.ts)
	if reg == "" {
		t.Fatal("регистрация другого пользователя не прошла")
	}
}

// Лимит на выдачу ws-тикетов по пользователю.
func TestWSTicketThrottle(t *testing.T) {
	e := newInterviewEnv(t)
	ticketURL := fmt.Sprintf("%s/api/v1/ws-ticket", e.ts.URL)
	h := map[string]string{"Authorization": "Bearer " + e.token}
	for i := 0; i < 60; i++ {
		code, _ := doJSON(t, "GET", ticketURL, nil, h)
		if code != http.StatusOK {
			t.Fatalf("выдача %d: %d", i+1, code)
		}
	}
	if code, _ := doJSON(t, "GET", ticketURL, nil, h); code != http.StatusTooManyRequests {
		t.Fatalf("61-я выдача: %d, хочу 429", code)
	}
}

// JWT в query больше не принимается: токен в URL живёт до конца TTL в логах
// caddy/nginx и в истории браузера, поэтому путь удалён, а не «оставлен на
// потом». Проверка, что он действительно закрыт.
func TestWSRejectsTokenInQuery(t *testing.T) {
	e := newInterviewEnv(t)
	u := url.URL{Scheme: "ws", Host: e.ts.Listener.Addr().String(),
		Path: fmt.Sprintf("/ws/session/%d", e.session), RawQuery: "token=" + url.QueryEscape(e.token)}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	_, resp, err := websocket.Dial(ctx, u.String(), nil)
	if err == nil {
		t.Fatal("апгрейд с ?token= прошёл — JWT в query всё ещё принимается")
	}
	if resp == nil || resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("ошибка = %v (ответ %v), хочу 401", err, resp)
	}
}
