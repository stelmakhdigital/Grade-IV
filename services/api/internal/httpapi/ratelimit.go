package httpapi

import (
	"bytes"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Ограничитель частоты. Нужен потому, что /auth/* открыты любому: без лимита
// подбор пароля идёт со скоростью сети (проверено: 8 неверных подряд — 401 без
// задержки, верный пароль сразу после них — 201/200), а каждый неверный стоит
// bcrypt ~60 мс CPU, то есть сам логин становится оружием.
//
// Ключи бывают двух родов, и это различие важно:
//   - email аккаунта и userID — их подделать нельзя, это настоящая граница;
//   - IP — best effort. Мы за nginx+caddy, а X-Forwarded-For клиент может
//     принести свой, и прокси его не выкидывает (nginx дописывает, а не
//     перезаписывает). Поэтому IP даёт грубую корзину, а не защиту: он режет
//     «один адрес долбит», но не «много адресов долбят».

// rateLimiter — скользящее окно на ключ: не больше limit событий за window.
type rateLimiter struct {
	mu     sync.Mutex
	hits   map[string][]time.Time
	limit  int
	window time.Duration
	now    func() time.Time
}

// sweepEvery — полная чистка карты. Без неё ключи, которые больше не
// появляются (ушёл кандидат, сменился IP), остались бы в памяти навсегда.
const sweepEvery = 4096

func newRateLimiter(limit int, window time.Duration) *rateLimiter {
	return &rateLimiter{hits: make(map[string][]time.Time), limit: limit, window: window, now: time.Now}
}

// allow — отметить событие по ключу. ok=false, если окно заполнено; retryS —
// сколько секунд ждать до освобождения самого раннего события.
func (l *rateLimiter) allow(key string) (ok bool, retryS int) {
	now := l.now()
	l.mu.Lock()
	defer l.mu.Unlock()

	seen := l.hits[key][:0]
	for _, t := range l.hits[key] {
		if now.Sub(t) < l.window {
			seen = append(seen, t)
		}
	}
	if len(seen) >= l.limit {
		l.hits[key] = seen
		// Осталось ждать до освобождения самого раннего события (округляем вверх,
		// но не больше окна).
		retry := int((l.window - now.Sub(seen[0]) + time.Second - time.Nanosecond) / time.Second)
		if retry < 1 {
			retry = 1
		}
		return false, retry
	}
	seen = append(seen, now)
	l.hits[key] = seen
	if len(seen) == 1 && len(l.hits) > sweepEvery {
		for k, v := range l.hits {
			if len(v) == 0 || now.Sub(v[len(v)-1]) >= l.window {
				delete(l.hits, k)
			}
		}
	}
	return true, 0
}

// rateLimited — middleware: 429 + Retry-After, если окно ключа заполнено.
// Пустой ключ (не удалось определить) — запрос пропускаем: лучше не впустить
// лимитер, чем не впустить пользователя из-за кривого заголовка.
func rateLimited(l *rateLimiter, key func(*http.Request) string, what string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := key(r)
		if k == "" {
			next.ServeHTTP(w, r)
			return
		}
		if ok, retry := l.allow(k); !ok {
			w.Header().Set("Retry-After", strconv.Itoa(retry))
			writeError(w, http.StatusTooManyRequests, "too_many_requests",
				"слишком много попыток («"+what+"»), попробуй через "+strconv.Itoa(retry)+" с")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// loginEmailKey — ключ логина: email аккаунта (он настоящая граница: какие бы
// адреса ни перебирал атакующий, стучится он в один аккаунт).
func loginEmailKey(r *http.Request) string {
	return "login:" + normalizedEmail(r)
}

// ipKey — корзина по адресу клиента; best effort (см. шапку файла).
func ipKey(r *http.Request) string {
	return "ip:" + clientIP(r)
}

// userKey — ключ для аутентифицированных маршрутов.
func userKey(r *http.Request) string {
	uid := userIDFromContext(r.Context())
	if uid == 0 {
		return ""
	}
	return "user:" + strconv.FormatInt(uid, 10)
}

// normalizedEmail — email из тела запроса в виде для ключа. Тело читается
// полностью и подменяется читателем, чтобы обработчик смог разобрать его
// заново (json.Decoder не умеет «подсмотреть и вернуть»).
func normalizedEmail(r *http.Request) string {
	if r.Body == nil {
		return ""
	}
	raw, err := io.ReadAll(io.LimitReader(r.Body, 1<<20))
	r.Body = io.NopCloser(bytes.NewReader(raw))
	if err != nil {
		return ""
	}
	var body struct {
		Email string `json:"email"`
	}
	if json.Unmarshal(raw, &body) != nil {
		return ""
	}
	return strings.ToLower(strings.TrimSpace(body.Email))
}

// clientIP — адрес из X-Forwarded-For (мы за reverse-proxy), иначе из соединения.
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		first, _, _ := strings.Cut(xff, ",")
		if ip := strings.TrimSpace(first); ip != "" {
			return ip
		}
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}
