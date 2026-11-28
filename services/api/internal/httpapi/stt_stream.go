package httpapi

import (
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Стрим-путь распознавания (voice /stt/stream, ADR-007): события стрима, barge-in,
// ход кандидата, режим записи и накопление отложенных реплик.

// onSTTStreamEvent — событие voice /stt/stream (ADR-007):
// "state" speech:true/false — отсчёт длительности реплики (в аудио-байтах,
// не wall — детерминировано при burst-отправке);
// "partial" — WS stt_partial (промежуточный текст в UI);
// "final" — ход кандидата (barge-in — если ИИ говорит);
// "unavailable" — деградация на batch-путь (Warn + метрика, однократно).
func (w *wsSession) onSTTStreamEvent(s *Server, ev voicesvc.StreamEvent) {
	switch ev.Type {
	case "unavailable":
		w.sttStreamDown.Store(true)
		// spec-14/A9: деградация стрима — флаги речи в пути сбрасываем, иначе
		// если voice умер во время речи кандидата (speech_start пришёл, end —
		// нет), isSpeaking() застревает: nudge не работает, barge-in навечно
		// считает речь активной.
		w.sttSpeechSince.Store(0)
		w.sttFinalPending.Store(false)
		if w.fallbackWarned.CompareAndSwap(false, true) {
			metrics.STTStreamFallbacks.Inc(nil)
			s.log.Warn("stt-стрим: voice /stt/stream недоступен — деградация на batch-путь (/stt)", "session", w.id)
		}
	case "state":
		// Границы речи Silero VAD (voice): barge-in во время речи — если ИИ
		// говорит и speech-сегмент длится ≥ BargeInMinSpeechMS без тишевой
		// границы — прерываем (тот же сценарий, что энергетический VAD-путь);
		// длительность для final — speech_ms из final-события.
		if ev.Speech {
			w.sttSpeechSince.Store(time.Now().UnixNano())
			w.sttFinalPending.Store(false)
			if w.ttsActive.Load() {
				time.AfterFunc(BargeInMinSpeechMS*time.Millisecond,
					func() { w.streamBargeIn(s) })
			}
		} else {
			w.sttSpeechSince.Store(0)
			// state=false → final уже считается в voice (STT) — финал в пути.
			w.sttFinalPending.Store(true)
		}
	case "partial":
		if ev.Text != "" {
			msg := map[string]any{"type": "stt_partial", "text": ev.Text}
			// Режим записи: к промежуточному тексту добавляем суммарную речь —
			// накопленное по финалам + текущий сегмент (speech_ms из partial:
			// длительность буфера на момент снимка). Иначе «речи N с» в окне
			// записи дожидалось границы тишины и приходило одним прыжком через
			// 2-3,6 с после паузы (ui-review 2026-11-28).
			if w.recording.Load() {
				msg["total_speech_ms"] = w.recSpeechMS.Load() + int64(ev.SpeechMS)
			}
			s.engine.SendTo(w.id, msg)
		}
	case "final":
		w.handleSTTStreamFinal(s, ev)
	}
}

// streamBargeIn — barge-in во время речи (стрим-путь): сработал
// BargeInMinSpeechMS после state=true при активном TTS и без тишевой
// границы. Однократно на TTS-стрим (sttBarged CAS; reset в beginTTS).
func (w *wsSession) streamBargeIn(s *Server) {
	if !w.ttsActive.Load() || w.sttSpeechSince.Load() == 0 {
		return
	}
	if !w.sttBarged.CompareAndSwap(false, true) {
		return
	}
	w.stopTTS()
	metrics.BargeInsTotal.Inc(nil)
	// Длительность речи (wall, от state=true): стрим-путь — реальное время
	// (аудио в реальном времени ⇒ wall ≈ аудио-длительность). Без pre-roll.
	ms := int(time.Since(time.Unix(0, w.sttSpeechSince.Load())).Milliseconds())
	metrics.BargeInSpeechMS.Observe(metrics.Labels{"result": "barge_in"}, float64(ms))
	s.log.Info("barge-in: кандидат прервал речь ИИ (стрим, во время речи)", "session", w.id, "ms", ms)
	s.engine.SendTo(w.id, map[string]any{"type": "tts_stop"})
}

// handleSTTStreamFinal — final из стримингового STT: ход кандидата.
// Barge-in: если ИИ говорит (ttsActive) и реплика ≥ BargeInMinSpeechMS
// (длительность — speech_ms из final, по Silero VAD на стороне voice) —
// прерывание речи ИИ (tts_stop) до старта хода; короткая реплика — отброс
// (защита от ложных срабатываний). Если streamBargeIn уже сработал
// (sttBarged) — дубль метрики/tts_stop не шлём, только ход.
func (w *wsSession) handleSTTStreamFinal(s *Server, ev voicesvc.StreamEvent) {
	text := strings.TrimSpace(ev.Text)
	ms := ev.SpeechMS
	w.sttFinalPending.Store(false)
	// Хвост диктовки (BUG-20261012-018): «recording off» пришёл во время речи
	// кандидата — этот final уже вошёл в склеенный буфер клиента (последний
	// partial) и не должен становиться отдельным сообщением («текст отправился
	// сам как в следующем сообщении»). Флаг одноразовый: новый разговорный
	// final после хвоста обрабатывается как обычно.
	if t := w.recTailDiscardAt.Load(); t != 0 {
		w.recTailDiscardAt.Store(0)
		if !w.recording.Load() && time.Since(time.Unix(0, t)) < recTailDiscardGrace {
			s.log.Info("recording: отброшен хвост диктовки (final после off)", "session", w.id, "chars", len(text))
			return
		}
	}
	// M5: режим записи — кандидат диктует ответ. ВСЕ его final-ы (включая короткие)
	// → в транскрипт (emitRecSegment), БЕЗ barge-in-гейта: шорт-фраза-дроп (эхо/
	// дыхание) + stopTTS на короткой реплике — voice-путь; в записи дроп коротких
	// фраз терял НАЧАЛО диктовки, пока ИИ ещё говорил (ttsActive) → при «Отправить»
	// уходил только хвост (видимый транскрипт длиннее отправленного).
	if w.recording.Load() {
		if text != "" {
			if snap, err := s.engine.Snapshot(w.id); err == nil && snap.Stage == models.StageVoice && snap.Status == models.StatusActive {
				w.emitRecSegment(s, text, ms)
			}
		}
		return
	}
	if w.ttsActive.Load() {
		if !w.sttBarged.Load() && ms < BargeInMinSpeechMS {
			// Короткая реплика — без прерывания, но с метрикой (result="ignored").
			metrics.BargeInSpeechMS.Observe(metrics.Labels{"result": "ignored"}, float64(ms))
			s.log.Debug("barge-in: короткая реплика (стрим), без прерывания", "session", w.id, "ms", ms)
			return // защита от ложных срабатываний (эхо, дыхание)
		}
		if w.sttBarged.CompareAndSwap(false, true) {
			w.stopTTS()
			metrics.BargeInsTotal.Inc(nil)
			metrics.BargeInSpeechMS.Observe(metrics.Labels{"result": "barge_in"}, float64(ms)) // speech_ms из Silero VAD (без pre-roll)
			s.log.Info("barge-in: кандидат прервал речь ИИ", "session", w.id, "ms", ms)
			s.engine.SendTo(w.id, map[string]any{"type": "tts_stop"})
		}
		if text == "" {
			return
		}
		go func() {
			// Прерванный ход отпустит busy после end-кадра (turnDone) — ждём,
			// иначе новый ход начнётся с занятым флагом и сразу закончится.
			for w.busy.Load() {
				select {
				case <-w.ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
			if !w.busy.CompareAndSwap(false, true) {
				return // на границе занята — реплика теряется (допустимо)
			}
			w.runStreamTurn(s, text, ev.Confidence, ev.SpeechMS)
		}()
		return
	}
	if w.sttBarged.Load() {
		// Barge-in уже сработал таймером (streamBargeIn): ttsActive сброшен,
		// прерванный ход ИИ ещё отпускает busy — ждём его (иначе CAS проигрывает
		// и реплика теряется).
		if text == "" {
			return
		}
		go func() {
			for w.busy.Load() {
				select {
				case <-w.ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
			if !w.busy.CompareAndSwap(false, true) {
				return // на границе занята — реплика теряется (допустимо)
			}
			w.runStreamTurn(s, text, ev.Confidence, ev.SpeechMS)
		}()
		return
	}
	if !w.busy.CompareAndSwap(false, true) {
		// Занят ход (LLM-фаза) — реплику НЕ бросаем: накапливаем (V1); flush после
		// хода (runStreamTurn) пошлёт её следом. Иначе длинная реплика кандидата
		// (много final-ов по VAD-границам/сегментации) терялась после первого сегмента,
		// а в UI застревал «распознаётся» (transcript для них не шёл).
		w.appendSTTPending(text)
		return
	}
	go w.runStreamTurn(s, text, ev.Confidence, ev.SpeechMS)
}

// emitRecSegment — режим записи (FR-S8, ADR-009): распознанный сегмент —
// в окно записи клиента (stt_segment), суммарная речь (total_speech_ms)
// — для клиентского критерия «недостаточно речи» (< 1.5 с). AI-ход НЕ
// стартует; barge-in-логика (tts_stop) при этом работает как обычно.
// Чанкование длинных записей: сегментация по тишине уже происходит в VAD
// voice (каждый final — отдельный сегмент, граница — на тиши); текст
// склеивается клиентом (KB-масштаб, без аудио-буфера).
func (w *wsSession) emitRecSegment(s *Server, text string, ms int) {
	if ms > 0 {
		w.recSpeechMS.Add(int64(ms))
	}
	s.log.Info("recording: сегмент транскрипта", "session", w.id, "chars", len(text), "speech_ms", ms, "total_speech_ms", w.recSpeechMS.Load())
	s.engine.SendTo(w.id, map[string]any{
		"type": "stt_segment", "text": text, "speech_ms": ms, "total_speech_ms": w.recSpeechMS.Load(),
	})
}

// runStreamTurn — ход кандидата из стримингового STT (busy уже занят;
// сбрасывается defer'ом): валидация стадии/доверия → runCandidateTurn.
func (w *wsSession) runStreamTurn(s *Server, text string, conf float64, speechMS int) {
	defer func() {
		w.busy.Store(false)
		w.flushSTTPending(s) // после хода — идём накопленными репликами (V1)
	}()
	if text == "" {
		return // молчание/шум — без хода
	}
	snap, err := s.engine.Snapshot(w.id)
	if err != nil || snap.Stage != models.StageVoice || snap.Status != models.StatusActive {
		return // голосовой конвейер работает только на активной voice-стадии
	}
	// Фильтр доверия (как в batch-пути): STT на шуме галлюцинирует с низкой
	// уверенностью; реальные реплики — conf ≥ 0.7 (замеры), порог 0.5.
	if conf < 0.5 {
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", w.id, "conf", conf, "text", truncateForLog(text, 32))
		return
	}
	// Режим записи (FR-S8): сегмент — в окно записи, AI-ход не стартует.
	if w.recording.Load() {
		w.emitRecSegment(s, text, speechMS)
		return
	}
	s.log.Info("stt: реплика кандидата", "session", w.id, "chars", len(text), "conf", conf, "stream", true)
	s.runCandidateTurn(w, text)
}

// appendSTTPending — накапливаем финал STT, пришедший пока занят ход (V1):
// длинная реплика режется VAD на несколько final-ов; следующие (пока идёт
// первый ход) не бросаются, а складываются — flushSTTPending пошлёт следом.
func (w *wsSession) appendSTTPending(text string) {
	t := strings.TrimSpace(text)
	if t == "" {
		return
	}
	w.sttPendingMu.Lock()
	w.sttPending += " " + t
	w.sttPendingMu.Unlock()
}

// flushSTTPending — после хода (busy освобождён) отправляем накопленные реплики
// следующим ходом (V1). conf высокий: текст уже распознан (не шум). Barge-in: если
// ИИ ещё говорит — прерываем (кандидат продолжает речь).
func (w *wsSession) flushSTTPending(s *Server) {
	w.sttPendingMu.Lock()
	p := strings.TrimSpace(w.sttPending)
	w.sttPending = ""
	w.sttPendingMu.Unlock()
	if p == "" {
		return
	}
	if w.ttsActive.Load() {
		// spec-14/A8: как в остальных barge-in точках (streamBargeIn и др.) —
		// stopTTS должен сопровождаться tts_stop клиенту, иначе после хода
		// кандидат продолжал слышать уже прерванные аудио-кадры; плюс метрика.
		w.stopTTS()
		metrics.BargeInsTotal.Inc(nil)
		s.log.Info("barge-in: отложенная реплика прервала речь ИИ", "session", w.id)
		s.engine.SendTo(w.id, map[string]any{"type": "tts_stop"})
	}
	if !w.busy.CompareAndSwap(false, true) {
		// редкая гонка (кто-то занял) — вернём в буфер, попробуем в следующий ход
		w.sttPendingMu.Lock()
		w.sttPending = p + " " + w.sttPending
		w.sttPendingMu.Unlock()
		return
	}
	s.log.Info("stt: отложенная реплика (была занята ходом) — отправляю", "session", w.id, "chars", len(p))
	go w.runStreamTurn(s, p, 0.95, 0)
}
