package httpapi

import (
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voiceorchestrator"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Подача PCM-кадров в VAD: каскад стримов и energy-путь (last-resort), pre-STT
// и завершение реплики — общие для всех путей.

// preSTTResult — предварительное распознавание (pre-STT): запущено на
// текущем буфере реплики при первой тишине (energy-путь: PreSilence,
// Silero-путь: pre_silence-событие), перекрывает остаток VAD-хвоста.
// Валидно, если реплика не расширялась после запуска.
type preSTTResult struct {
	res     voicesvc.STTResult
	errored bool
	done    chan struct{}
}

// preSTTFor — предварительное распознавание буфера реплики (общий для
// energy-пути (PreSilence) и Silero-пути (pre_silence-событие)): перекрывает
// остаток VAD-хвоста, экономит время STT в пайплайне (~0.7 с на CPU).
// Инвалидация: реплика расширилась после запуска (preSTTBytes не совпадёт).
func (s *Server) preSTTFor(ws *wsSession, buf []byte) {
	if len(buf) == 0 || s.voice == nil {
		return
	}
	if !ws.preSTTActive.CompareAndSwap(false, true) {
		return // pre-STT уже запущен и не сброшен
	}
	ws.preSTTBytes.Store(int64(len(buf)))
	p := &preSTTResult{done: make(chan struct{})}
	ws.setPreSTT(p)
	go func() {
		defer close(p.done)
		r, err := s.voice.STT(ws.ctx, buf)
		if err != nil {
			p.errored = true
			s.log.Debug("stt: pre-распознавание не удалось", "session", ws.id, "err", err)
			return
		}
		p.res = r
	}()
	s.log.Debug("stt: pre-распознавание запущено", "session", ws.id, "bytes", len(buf))
}

// completeUtterance — завершённая реплика (energy-путь: VAD-хвост; Silero-путь:
// utterance-событие /vad/stream). Общее: сброс pre-STT-флага, barge-in-проверка
// (ttsActive + BargeInMinSpeechMS), busy-CAS, ход кандидата (handleVoiceUtterance).
// Поведение energy-пути не меняется (DRY-вынос). prerollMS — доля pre-roll в
// буфере (Silero-путь: до 1 с тишины перед речью; energy-путь: 0) — вычитается
// при barge-in пороге (решение #41: короткая речь с pre-roll не прерывает TTS).
func (s *Server) completeUtterance(ws *wsSession, utterance []byte, prerollMS int) {
	// Реплика завершена: сбрасываем pre-STT-флаг (если ещё не сброшен).
	ws.preSTTActive.Store(false)
	if ws.ttsActive.Load() {
		// Кандидат заговорил, пока ИИ говорит. Порог barge-in — по длительности
		// РЕЧИ (без pre-roll): totalMS = весь буфер (Silero включает pre-ring),
		// ms = речь. Короткая речь (< BargeInMinSpeechMS) не прерывает, даже если
		// pre-roll в буфере «добил» totalMS до порога (pre-roll-риск, решение #40).
		totalMS := int(int64(len(utterance)) * 1000 / 2 / 16000)
		// Решение о barge-in — voiceorchestrator (чистая state-машина, A1):
		// ms = речь (totalMS − pre-roll), порог BargeInMinSpeechMS.
		action, ms := voiceorchestrator.BargeInDecision(ws.ttsActive.Load(), totalMS, prerollMS)
		if action != voiceorchestrator.BargeInTrigger {
			// Короткая речь (или речи нет) не прерывает — но несёт метрику
			// (result="ignored"): наблюдаемость вычитания pre-roll и порога
			// (T-20261009152316). Защита от ложных срабатываний (эхо, дыхание).
			metrics.BargeInSpeechMS.Observe(metrics.Labels{"result": "ignored"}, float64(ms))
			s.log.Debug("barge-in: короткая реплика, без прерывания", "session", ws.id, "ms", ms, "preroll_ms", prerollMS)
			return
		}
		ws.stopTTS()
		metrics.BargeInsTotal.Inc(nil)
		metrics.BargeInSpeechMS.Observe(metrics.Labels{"result": "barge_in"}, float64(ms))
		s.log.Info("barge-in: кандидат прервал речь ИИ", "session", ws.id, "ms", ms, "preroll_ms", prerollMS)
		s.engine.SendTo(ws.id, map[string]any{"type": "tts_stop"})
		go func() {
			// Прерванный ход отпустит busy после end-кадра (turnDone) — ждём,
			// иначе новый ход начнётся с занятым флагом и сразу закончится.
			for ws.busy.Load() {
				select {
				case <-ws.ctx.Done():
					return
				case <-time.After(10 * time.Millisecond):
				}
			}
			if !ws.busy.CompareAndSwap(false, true) {
				return // на границе занята — реплика теряется (допустимо)
			}
			s.handleVoiceUtterance(ws, utterance)
		}()
		return
	}
	if !ws.busy.CompareAndSwap(false, true) {
		return // на границе занят — реплика теряется (допустимо в ходовом режиме)
	}
	s.log.Debug("vad: реплика завершена", "session", ws.id, "bytes", len(utterance))
	go s.handleVoiceUtterance(ws, utterance)
}

// feedVAD — бинарный кадр PCM из readLoop.
// Цепочка деградации (3 уровня, ADR-002 поправка 2026-10-09):
//
//	(1) voice /stt/stream (Silero VAD + стриминговый STT, ADR-007, дефолт);
//	(2) voice /vad/stream (Silero VAD) + batch /stt — кадры в VADStream, реплики
//	    обрабатываются onVADStreamEvent (pre-STT на pre_silence);
//	(3) energy VAD в Go + batch /stt (feedVADBatch) — last-resort.
//
// Переход на следующий уровень — Warn + метрика (однократно, в on*-event).
// Turn-taking общий для всех путей: пока ход занят И ИИ не говорит
// (LLM-фаза) — кадры не слушаются.
func (s *Server) feedVAD(ws *wsSession, pcm []byte) {
	// Пауза/завершение: голос кандидата не обрабатывается (FR-S7) — кадры не
	// слушаем (VAD не накапливает, стримы не получают: нет ходов «из паузы»
	// и после возобновления).
	if snap, err := s.engine.Snapshot(ws.id); err != nil || snap.Status != models.StatusActive {
		return
	}
	// Ход занят, но ИИ молчит (LLM-фаза) — не слушаем (turn-taking, ADR-002).
	// Исключение — режим записи (BUG-20261012-018): кандидат диктует ответ,
	// и отбрасывать его аудио на ~8 с LLM-фазы = терять начало ответа
	// («наговариваю, а через абзац перестаёт записываться»). В записи final-ы
	// идут в буфер (emitRecSegment), ход не стартуют — turn-taking не нужен.
	if ws.busy.Load() && !ws.ttsActive.Load() && !ws.recording.Load() {
		return
	}
	if !ws.sttStreamDown.Load() {
		if st := ws.sttStreamFor(s); st != nil {
			st.Send(pcm)
			return
		}
	}
	if !ws.vadStreamDown.Load() {
		if vs := ws.vadStreamFor(s); vs != nil {
			vs.Send(pcm)
			return
		}
	}
	s.feedVADBatch(ws, pcm)
}

// feedVADBatch — energy-путь (level 3, last-resort: voice недоступен):
// локальный энергетический VAD; по завершённой реплике — конвейер
// (preSTTFor + completeUtterance — общие с Silero-путом, DRY).
func (s *Server) feedVADBatch(ws *wsSession, pcm []byte) {
	utterance, done := ws.vad.Feed(pcm)
	if !done {
		// Pre-STT: предварительное распознавание при первой тишине.
		if ws.vad.PreSilence() {
			s.preSTTFor(ws, ws.vad.Utterance())
		}
		return
	}
	s.completeUtterance(ws, utterance, 0) // energy-путь: pre-roll в реплику не входит
}

// onVADStreamEvent — событие voice /vad/stream (Silero VAD, ADR-002
// поправка 2026-10-09): speech_start → «кандидат говорит» (анти-nudge);
// pre_silence → pre-STT на текущий буфер; utterance → ход кандидата
// (barge-in по BargeInMinSpeechMS, как energy-путь); unavailable →
// деградация на energy-путь (Warn + метрика, однократно).
func (w *wsSession) onVADStreamEvent(s *Server, ev voicesvc.VADEvent) {
	switch ev.Type {
	case "unavailable":
		w.vadStreamDown.Store(true)
		// spec-14/A9: см. onSTTStreamEvent — сброс флага речи в пути.
		w.sileroSpeech.Store(false)
		if w.vadFallbackWarned.CompareAndSwap(false, true) {
			metrics.VADStreamFallbacks.Inc(nil)
			s.log.Warn("vad-стрим: voice /vad/stream недоступен — деградация на energy-путь", "session", w.id)
		}
	case "speech_start":
		w.sileroSpeech.Store(true)
	case "pre_silence":
		s.preSTTFor(w, ev.Utterance)
	case "utterance":
		w.sileroSpeech.Store(false)
		s.completeUtterance(w, ev.Utterance, ev.PreRollMS) // Silero-путь: вычитать pre-roll
	}
}
