// Маршруты HTTP API: таблица маршрутов Server.Handler и health-check.
// Отдельный файл: сборка Server (server.go) и список маршрутов читаются независимо.

package httpapi

import (
	"net/http"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
)

// Handler — корневой обработчик с маршрутами.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", s.handleHealth)
	// Dev-эндпоинты диагностики — только при явном ENABLE_DEBUG=1 (в prod — 404,
	// без аутентификации наружу не выставляем).
	if s.cfg.DebugEndpoints {
		mux.HandleFunc("POST /debug/mic-report", s.handleDebugMicReport)
		mux.HandleFunc("OPTIONS /debug/mic-report", s.handleDebugMicReport)
	}
	// /metrics — под basic-auth (METRICS_USER/METRICS_PASSWORD). Без пароля
	// маршрута нет вовсе: тихая утечка метрик хуже, чем красный target у
	// prometheus'а — тот сразу виден в /targets.
	if s.cfg.MetricsPassword != "" {
		mux.Handle("GET /metrics", requireBasicAuth(s.cfg.MetricsUser, s.cfg.MetricsPassword, http.HandlerFunc(metrics.Handler)))
	}
	mux.Handle("POST /api/v1/auth/register",
		rateLimited(s.registerLimit, ipKey, "регистраций с этого адреса", http.HandlerFunc(s.handleRegister)))
	// Вход ограничивается дважды: по аккаунту (перебор чужого пароля) и по
	// адресу (грубая корзина: XFF клиент может принести свой, см. ratelimit.go).
	mux.Handle("POST /api/v1/auth/login",
		rateLimited(s.loginLimit, loginEmailKey, "входов в этот аккаунт",
			rateLimited(s.authIPLimit, ipKey, "входов с этого адреса",
				http.HandlerFunc(s.handleLogin))))
	mux.Handle("GET /api/v1/auth/me", s.requireAuth(http.HandlerFunc(s.handleMe)))
	// F6 (итерация 12): вакансия/кампания кандидата (профиль → system-промпт).
	mux.Handle("GET /api/v1/me/vacancy", s.requireAuth(http.HandlerFunc(s.handleGetVacancy)))
	mux.Handle("PUT /api/v1/me/vacancy", s.requireAuth(http.HandlerFunc(s.handlePutVacancy)))
	// Резюме кандидата (описание + файл): профиль для ИИ и подбор роли/грейда.
	mux.Handle("GET /api/v1/me/resume", s.requireAuth(http.HandlerFunc(s.handleGetResume)))
	mux.Handle("PUT /api/v1/me/resume", s.requireAuth(http.HandlerFunc(s.handlePutResume)))
	mux.Handle("DELETE /api/v1/me/resume", s.requireAuth(http.HandlerFunc(s.handleDeleteResume)))

	// Шаблоны интервью (итерация A).
	mux.Handle("GET /api/v1/templates", s.requireAuth(http.HandlerFunc(s.handleTemplatesList)))

	// Сессии (WP-3, ARCHITECTURE.md §4.1).
	mux.Handle("POST /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsCreate)))
	mux.Handle("GET /api/v1/tts-voices", s.requireAuth(http.HandlerFunc(s.handleTTSVoices))) // F1: реестр голосов
	mux.Handle("GET /api/v1/profiles", s.requireAuth(http.HandlerFunc(s.handleProfilesList)))
	mux.Handle("GET /api/v1/profiles/{id}", s.requireAuth(http.HandlerFunc(s.handleProfileGet)))
	mux.Handle("GET /api/v1/sessions", s.requireAuth(http.HandlerFunc(s.handleSessionsList)))
	mux.Handle("GET /api/v1/sessions/{id}", s.requireAuth(http.HandlerFunc(s.handleSessionGet)))
	mux.Handle("POST /api/v1/sessions/{id}/{action}", s.requireAuth(http.HandlerFunc(s.handleSessionAction)))
	// Более специфичный путь (литеральный сегмент) — приоритет над {action}.
	mux.Handle("POST /api/v1/sessions/{id}/runs", s.requireAuth(http.HandlerFunc(s.handleSessionRuns)))
	mux.Handle("GET /api/v1/sessions/{id}/events", s.requireAuth(http.HandlerFunc(s.handleSessionEvents)))
	mux.Handle("PUT /api/v1/sessions/{id}/whiteboard", s.requireAuth(http.HandlerFunc(s.handleWhiteboardPut)))
	mux.Handle("GET /api/v1/sessions/{id}/whiteboard", s.requireAuth(http.HandlerFunc(s.handleWhiteboardGet)))
	mux.Handle("GET /api/v1/sessions/{id}/report", s.requireAuth(http.HandlerFunc(s.handleReportGet)))
	// F1-export (spec-16): публичная ссылка на отчёт — токен выдаёт владелец,
	// публичный роут отдаёт отчёт без аутентификации (без персональных данных).
	mux.Handle("POST /api/v1/sessions/{id}/share", s.requireAuth(http.HandlerFunc(s.handleSessionShare)))
	mux.Handle("DELETE /api/v1/sessions/{id}/share", s.requireAuth(http.HandlerFunc(s.handleSessionShareRevoke)))
	mux.Handle("GET /api/v1/public/reports/{token}", http.HandlerFunc(s.handlePublicReport))
	// spec-14/#12: одноразовый тикет WS-upgrade (JWT больше не в query).
	mux.Handle("GET /api/v1/ws-ticket", s.requireAuth(
		rateLimited(s.wsTicketLimit, userKey, "выдач токена подключения",
			http.HandlerFunc(s.handleWSTicket))))
	// spec-18: треки собеседований (кабинет) + admin-CRUD.
	mux.Handle("GET /api/v1/tracks", s.requireAuth(http.HandlerFunc(s.handleTracksList)))
	mux.Handle("GET /api/v1/admin/tracks", s.requireAdmin(http.HandlerFunc(s.handleAdminTracksList)))
	mux.Handle("POST /api/v1/admin/tracks", s.requireAdmin(http.HandlerFunc(s.handleAdminTrackCreate)))
	// spec-22: черновик роли по описанию (ИИ) — не сохраняет, только draft.
	mux.Handle("POST /api/v1/admin/tracks/generate", s.requireAdmin(http.HandlerFunc(s.handleAdminTrackGenerate)))
	mux.Handle("PUT /api/v1/admin/tracks/{id}", s.requireAdmin(http.HandlerFunc(s.handleAdminTrackUpdate)))
	mux.Handle("DELETE /api/v1/admin/tracks/{id}", s.requireAdmin(http.HandlerFunc(s.handleAdminTrackDelete)))

	// Admin (итерация 11): редактирование ВСЕХ шаблонов проекта. Только is_admin
	// (requireAdmin = requireAuth + 403 для не-admin).
	mux.Handle("GET /api/v1/admin/templates", s.requireAdmin(http.HandlerFunc(s.handleAdminTemplates)))
	mux.Handle("POST /api/v1/admin/templates", s.requireAdmin(http.HandlerFunc(s.handleAdminTemplateCreate)))
	mux.Handle("PUT /api/v1/admin/templates/{id}", s.requireAdmin(http.HandlerFunc(s.handleAdminTemplateUpdate)))
	mux.Handle("DELETE /api/v1/admin/templates/{id}", s.requireAdmin(http.HandlerFunc(s.handleAdminTemplateDelete)))
	mux.Handle("GET /api/v1/admin/openings", s.requireAdmin(http.HandlerFunc(s.handleAdminOpenings)))
	mux.Handle("PUT /api/v1/admin/openings", s.requireAdmin(http.HandlerFunc(s.handleAdminOpeningsPut)))
	mux.Handle("GET /api/v1/admin/tts-voices", s.requireAdmin(http.HandlerFunc(s.handleAdminTTSVoices)))
	mux.Handle("PUT /api/v1/admin/tts-voices", s.requireAdmin(http.HandlerFunc(s.handleAdminTTSVoicesPut)))
	// Контур настроек (фидбек 2026-11-28): что подключено + выбор моделей.
	mux.Handle("GET /api/v1/admin/runtime", s.requireAdmin(http.HandlerFunc(s.adminRuntime)))
	mux.Handle("PUT /api/v1/admin/models", s.requireAdmin(http.HandlerFunc(s.adminModelsPut)))

	// Голосовой канал (WP-3, ADR-001): auth — токен в query/заголовке.
	mux.HandleFunc("GET /ws/session/{id}", s.handleSessionWS)
	return s.withLogging(mux)
}
func (s *Server) handleHealth(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok", "service": "grade-api"})
}
