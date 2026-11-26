// Package voiceorchestrator — голосовой контур: чистая логика (TTS-текст,
// клиуз-дробление, barge-in) и DI-оркестратор хода VAD→STT→LLM→TTS.
//
// Вынесен из httpapi (A1, docs/history/ITERATIONS.md): раньше вся логика была
// методами *Server/*wsSession в internal/httpapi (тогда voice_pipeline.go на
// ~1045 строк; сейчас в httpapi остались transport-состояние и тонкие
// делегаты). Здесь:
//   - stateless-логика (SplitForTTS, TTSGap, PrepareTTS, TruncateForLog, …) —
//     чистые, unit-тестируемые функции;
//   - BargeIn — state-машина barge-in/pause (чистая функция входов);
//   - Orchestrator — DI (New(Deps): Engine/Voice/Interviewer/Log) — конвейер
//     хода (LLM-стрим → TTS-кадры с pacing, barge-in, pause/resume).
//
// Транспорт (WebSocket) остаётся в httpapi: состояние соединения — wsSession
// (ws.go), делегирование в Orchestrator — вызовы из voice_pipeline.go.
// Поведение не менялось — существующие integration-тесты httpapi проходят
// без изменений.
package voiceorchestrator
