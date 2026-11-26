package httpapi

import (
	"crypto/subtle"
	"net/http"
)

// requireBasicAuth — basic-auth для /metrics.
//
// Метрики — не публичный интерфейс: пути, идентификаторы сессий, лаги голосового
// контура. Наружу api в прод-профиле не публикуется, но prometheus скрейпит
// api:8000 изнутри docker-сети, и до этой проверки там читал кто угодно, кто
// поднял контейнер в той же сети.
//
// Сверка через ConstantTimeCompare: время ответа не должно выдавать, какая из
// двух строк совпала.
func requireBasicAuth(user, pass string, next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUser, gotPass, ok := r.BasicAuth()
		same := subtle.ConstantTimeCompare([]byte(gotUser), []byte(user)) &
			subtle.ConstantTimeCompare([]byte(gotPass), []byte(pass))
		if !ok || same != 1 {
			w.Header().Set("WWW-Authenticate", `Basic realm="metrics", charset="UTF-8"`)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		next.ServeHTTP(w, r)
	})
}
