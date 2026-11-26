# BUG-20261011-012 · F1/F2: config-файлы не грузятся при запуске из корня репо

- **Статус:** исправлено
- **Область:** `services/api/internal/httpapi/tts_voices.go`, `services/api/internal/httpapi/server.go`

## Симптом
В кабинете нет селектора «Голос интервьюера» (F1) и случайные начала собеседования (F2)
не применяются — хотя обе фичи реализованы, протестированы и закоммичены (Итерация 10).

## Шаги воспроизведения
1. Поднять стек штатно: `bash scripts/run-all.sh start` (api стартует с CWD = корень репо).
2. Открыть кабинет → селектора голоса нет.
3. `GET /api/v1/tts-voices` → 500 `«не удалось загрузить реестр голосов»`.

## Корень
Дефолтные пути к config-файлам были **CWD-зависимыми**:
- `TTS_VOICES_FILE` дефолт — `config/tts-voices.json` (относительно CWD);
- `INTRO_OPENINGS_FILE` дефолт — `config/openings.json`.

`make run-api` стартует api из `services/api` — файл находился. `scripts/run-all.sh`
стартует из корня репо (`cd "$ROOT"`, строка запуска api) — `config/tts-voices.json`
относительно корня не существует:
- F1: `os.ReadFile` → ошибка → 500 → frontend получает error и скрывает селектор
  (`ttsVoices.length > 0` не выполняется);
- F2: `LoadOpenings` при `os.IsNotExist` **тихо** возвращал `(nil, nil)` без лога —
  интервью начиналось дефолтным приветствием, ошибка не видна в логах.

## Фикс
- `configCandidates(base)` — кандидаты пути: `base` (CWD=services/api) и
  `services/api/base` (CWD=корень репо). ENV-переопределение (`TTS_VOICES_FILE`,
  `INTRO_OPENINGS_FILE`) имеет приоритет и пробует ровно указанный путь.
- `loadTTSVoices`: перебор кандидатов; ошибка со списком пробованных путей (не
  «open config/...: no such file»).
- Загрузка openings: `firstExistingConfig` по кандидатам; если файл не найден ни в
  одном месте — `LoadOpenings` не вызывается (то же поведение дефолтного начала, но
  без ложного `open ""`-пути).

## Проверка
- `go build ./...`, `go vet`, `go test ./internal/httpapi/` — ok (63 c).
- Live (CWD=корень репо): `GET /api/v1/tts-voices` → 200, реестр из
  `services/api/config/tts-voices.json`; в логе api ошибок нет.
