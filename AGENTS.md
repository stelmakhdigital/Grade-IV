# Правила проекта

## STT: только GigaAM

STT-провайдер по умолчанию (dev и прод) — **GigaAM v3**
(`VOICE_STT_PROVIDER=gigaam`, модель `v3_e2e_rnnt`). faster-whisper — не
дефолт, а запасная опция (GPU-узел, dev без torch); включать только
осознанно и по запросу, не «чтобы заработало».

Почему: на русском WER GigaAM v3 RNNT ~8-11 против ~21 у Whisper large-v3
(таблица salute-developers/GigaAM evaluation.md), плюс быстрее на CPU.
Обоснование в коде — `services/voice/app/gigaam.py`, коммит `2e1a088`.

Что не должно «починить» дефолт обратно на whisper:
- Пакет gigaam ставится из git, не с PyPI: на PyPI только 0.1.0, в нём нет
  v3-моделей (`v3_e2e_rnnt` отсутствует). Закреплённая строка — в
  `services/voice/requirements.txt`; там же отдельной строкой `torchaudio`
  (gigaam импортирует его при загрузке модуля, а в базовых зависимостях 0.2.0
  его нет — он только в extra `[torch]`).
- `torch` и `torchaudio` на CPU-машинах ставятся ДО `requirements.txt` с
  CPU-индекса (`make install`, CI-джоба voice) — иначе pip тянет nvidia-*.
- Модель обязательна: без `models/stt-gigaam/v3_e2e_rnnt.ckpt` STT молчит
  (контракт — `text=""`, не 500), `GET /api/v1/health` при этом показывает
  `stt.provider=gigaam, loaded=false`, а на старте voice — ERROR с точным
  путём. Канон каталога — `<репо>/models` (его монтирует prod-compose в
  `/models`); в dev `run-all.sh` сам указывает `MODELS_DIR=$PWD/services/voice/models`
  и `GIGAAM_DOWNLOAD_ROOT` туда же. Скачивание: `bash scripts/download-models.sh`
  (по умолчанию качает в `<репо>/models`).
- Качать надо заранее: у Sber-CDN сертификат просрочен (notAfter 2026-11-04),
  curl ещё проходит, а urllib внутри `gigaam.load_model` — нет
  (`CERTIFICATE_VERIFY_FAILED`), то есть автоскачивание модели в рантайме
  мертво. Зеркало — `GIGAAM_URL` (см. `scripts/download-models.sh`), пакет
  сам сверяет md5 ckpt.
- Дефолты держим в одном месте: `services/voice/app/main.py` (код),
  `scripts/run-all.sh` (dev-запуск), `infra/docker-compose.yml` (прод),
  `.env.example`. Расходятся — править в сторону gigaam, а не наоборот.
