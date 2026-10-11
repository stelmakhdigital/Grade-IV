package httpapi

import (
	"context"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Conn-адаптеры voiceorchestrator (A1): wsSession реализует
// voiceorchestrator.Conn. Вынесены в отдельный файл (не ws.go), чтобы ws.go
// оставался чистым транспортным слоем: каждый метод тонкий — делегирует в поле
// wsSession или в существующие beginTTS/endTTS.

// ID — id сессии (для engine.SendBinary/SendTo и логов/метрик).
func (w *wsSession) ID() int64 { return w.id }

// Ctx — контекст подключения (отмена = конец сессии) + F1: голос интервьюера
// (выбран в кабинете, из сессии) — voicesvc.Client.TTS читает его из контекста и
// кладёт в запрос ("speaker"). Пустой speaker — контекст без изменений (дефолт).
func (w *wsSession) Ctx() context.Context { return voicesvc.WithTTSSpeaker(w.ctx, w.speaker) }

// SessStopped — пауза/завершение сессии (FR-S7): голосовой конвейер останавливается.
func (w *wsSession) SessStopped() bool { return w.sessStopped.Load() }

// BeginTTS — начать TTS-стрим: возвращает stop-канал (закрывается при barge-in).
func (w *wsSession) BeginTTS() <-chan struct{} { return w.beginTTS() }

// EndTTS — завершить TTS-стрим (сброс ttsActive после end-кадра).
func (w *wsSession) EndTTS(stop <-chan struct{}) { w.endTTS(stop) }
