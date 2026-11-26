package httpapi

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestRequireBasicAuth(t *testing.T) {
	inner := 0
	h := requireBasicAuth("prom", "s3cret", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		inner++
		w.WriteHeader(http.StatusOK)
	}))

	cases := []struct {
		name       string
		user, pass string
		send       bool
		wantStatus int
	}{
		{"без заголовка", "", "", false, http.StatusUnauthorized},
		{"верные", "prom", "s3cret", true, http.StatusOK},
		{"чужой пользователь", "root", "s3cret", true, http.StatusUnauthorized},
		{"неверный пароль", "prom", "admin", true, http.StatusUnauthorized},
		{"пустой пароль в запросе", "prom", "", true, http.StatusUnauthorized},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodGet, "/metrics", nil)
			if tc.send {
				req.SetBasicAuth(tc.user, tc.pass)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("статус %d, want %d", rec.Code, tc.wantStatus)
			}
			if tc.wantStatus == http.StatusUnauthorized {
				if rec.Header().Get("WWW-Authenticate") == "" {
					t.Fatal("нет WWW-Authenticate — клиент не поймёт, что нужен basic-auth")
				}
			}
		})
	}
	if inner != 1 {
		t.Fatalf("внутренний обработчик вызван %d раз, want 1 (только на верных данных)", inner)
	}
}

// Маршрут появляется только с паролем: без него метрики не читаются вовсе,
// а prometheus честно показывает мишень как недоступную.
func TestMetricsRouteGatedByPassword(t *testing.T) {
	t.Run("с паролем — 401 без учётных данных, 200 с ними", func(t *testing.T) {
		e := newInterviewEnv(t)
		resp, err := http.Get(e.ts.URL + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusUnauthorized {
			t.Fatalf("без учётных данных %d, want 401", resp.StatusCode)
		}

		req, _ := http.NewRequest(http.MethodGet, e.ts.URL+"/metrics", nil)
		req.SetBasicAuth("prom", "test-metrics")
		authed, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer authed.Body.Close()
		if authed.StatusCode != http.StatusOK {
			t.Fatalf("с учётными данными %d, want 200", authed.StatusCode)
		}
	})

	t.Run("без пароля — маршрута нет", func(t *testing.T) {
		e := newInterviewEnv(t)
		e.srv.cfg.MetricsPassword = ""
		ts := httptest.NewServer(e.srv.Handler())
		defer ts.Close()
		resp, err := http.Get(ts.URL + "/metrics")
		if err != nil {
			t.Fatal(err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusNotFound {
			t.Fatalf("без METRICS_PASSWORD ждём 404, получили %d", resp.StatusCode)
		}
	})
}
