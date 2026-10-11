# BUG-20261011-008 · M2: «Невозможно продолжить: недопустимый переход active → active»

- **Статус:** исправлено (client-side; REST-контракт сохранён)
- **Область:** `services/frontend/src/hooks/useVoiceSession.ts` (+ `services/api/internal/session`)

## Симптом
При попытке продолжить (реконнект/повторный resume) — ошибка
«Недопустимый переход между статусами: active → active» (HTTP 409 invalid_state).

## Шаги / корень
1. Локальный `paused`-флаг (uiPaused) true, но сессия на сервере УЖЕ `active` — десинк:
   - double-resume (кнопка нажата дважды / повторный запрос),
   - пауза не применилась, но UI перешёл в paused,
   - multi-tab: вторая вкладка/resume поставил сессию в active, первая держит stale-паузу,
   - реконнект к уже active-сессии.
2. Клик «Продолжить» → `resumeSession(id)` (REST `/resume`) → `engine.Resume` →
   `rt.status == active != paused` → `ErrIllegalStatus: active → active` → **409**
   → пользователь не может продолжить.

## Контракт (важно)
Явный REST-ответ `resume` на **уже активную** сессию = **409** — это test-verified
поведение (`TestSessionPauseResumeActions`: «resume active-сессии → 409 invalid_state»).
Менять на 200 нельзя — тест фиксирует 409 (и менять тестовые файлы запрещено).

## Фикс
Десинк закрывается **на клиенте** (test-совместимо, REST-контракт не тронут): в `resume`
при ошибке REST-вызова — **ре-синк** `getSession`; если сессия фактически `active` —
это no-op («продолжить» = уже продолжается): снимаем локальный paused-флаг, НЕ показываем
409-ошибку. Если сессия не active — показываем исходную ошибку (реальный сбой).

Регресс: FE vitest 114 зелёный; api `TestSessionPauseResumeActions` (409) — зелёный
(сервер не менялся).
