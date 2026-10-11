// Package voiceorchestrator — голосовой контур: чистая логика (TTS-текст,
// клиуз-дробление, barge-in) и DI-оркестратор хода VAD→STT→LLM→TTS.
//
// Вынесен из httpapi (A1, plan.md): раньше вся логика была методами *Server/
// *wsSession в internal/httpapi (voice_pipeline.go ~1045 строк). Здесь:
//   - stateless-логика (SplitForTTS, TTSGap, PrepareTTS, TruncateForLog, …) —
//     чистые, unit-тестируемые функции;
//   - BargeIn — state-машина barge-in/pause (чистая функция входов);
//   - Orchestrator — DI (New(Deps): Engine/Voice/Interviewer/Log) — конвейер
//     хода (LLM-стрим → TTS-кадры с pacing, barge-in, pause/resume).
//
// Транспорт (WebSocket) остаётся в httpapi: ws.go — тонкий слой, делегирующий
// в Orchestrator. Поведение не меняется — существующие integration-тесты
// httpapi проходят без изменений.
package voiceorchestrator
