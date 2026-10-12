package httpapi

import (
	"strings"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voiceorchestrator"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Пайплайн голосового хода (ADR-002): PCM-кадры кандидата → VAD → voice /stt →
// движок интервьюера (LLM, SSE-стрим) → voice /tts → бинарные кадры PCM16 по WS.
//
// A1 (voiceorchestrator): чистая голосовая логика (TTS-текст/клиузы/barge-in) и
// DI-конвейер хода (LLM→TTS: pacing, ordered-синтез) вынесены в пакет
// internal/voiceorchestrator. Здесь остаётся transport-зависимое (wsSession-
// состояние: pre-STT, стримы VAD/STT, recording) + тонкие делегаты в оркестратор.

const (
	// BargeInMinSpeechMS — мин. длительность речи кандидата (мс) для barge-in
	// (защита от ложных срабатываний; Silero-путь: таймер в onSTTStreamEvent).
	// Единственный источник — voiceorchestrator.
	BargeInMinSpeechMS = voiceorchestrator.BargeInMinSpeechMS
	// recTailDiscardGrace — окно, в которое final после «recording off» при
	// активной речи считается хвостом диктовки (BUG-20261012-018). Покрывает
	// время STT хвостовой реплики (≤ 30 с аудио: ~5 с на CPU, ~1 с на GPU).
	recTailDiscardGrace = 15 * time.Second
	// MIN_CLAUSE_CHARS — мин. длина клиаза (байты) для клиуз-дробления (A1: единый
	// источник в voiceorchestrator; сохранён для unit-тестов httpapi).
	MIN_CLAUSE_CHARS = voiceorchestrator.MinClauseChars
)

// runCandidateTurn — ход кандидата: делегирование в DI-оркестратор (A1).
func (s *Server) runCandidateTurn(ws *wsSession, text string) {
	s.orch.RunCandidateTurn(ws, s.orchVoice(), text)
}

// streamAIAudio — озвучка текста ИИ (greeting/подсказка, FR-S3): делегирование
// в оркестратор (TTS по предложениям + pacing, end-флаг на последнем кадре).
func (s *Server) streamAIAudio(ws *wsSession, text string) {
	s.orch.StreamAIAudio(ws, s.orchVoice(), text)
}

// orchVoice — текущий TTS-клиент как voiceorchestrator.Voice (ниль = без
// озвучки, только текст). Читается при вызове (не кэшируется): тесты подменяют
// s.voice после NewWithLLM — оркестратор должен видеть актуальный клиент.
func (s *Server) orchVoice() voiceorchestrator.Voice {
	if s.voice == nil {
		return nil
	}
	return s.voice
}

// --- Тонкие обёртки чистой TTS-логики (реализация в voiceorchestrator, A1). ---
// Сохранены для существующих unit-тестов httpapi (TestSplit*/TestTTSGap/…);
// логика дублирования нет — только делегирование.

func splitSentences(text string) []string { return voiceorchestrator.SplitSentences(text) }

func splitSentencesTail(text string) ([]string, string) {
	return voiceorchestrator.SplitSentencesTail(text)
}

func splitForTTS(text string) []string { return voiceorchestrator.SplitForTTS(text) }

func splitForTTSStreaming(text string) ([]string, string) {
	return voiceorchestrator.SplitForTTSStreaming(text)
}

func splitClauses(part string) []string { return voiceorchestrator.SplitClauses(part) }

func splitClausesWithTail(part string) ([]string, string) {
	return voiceorchestrator.SplitClausesWithTail(part)
}

func hasWordChar(s string) bool { return voiceorchestrator.HasWordChar(s) }

func ttsGap(prev string) int { return voiceorchestrator.TTSGap(prev) }

func isUpperNext(text string, i int) bool { return voiceorchestrator.IsUpperNext(text, i) }

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

// handleVoiceUtterance — goroutine: завершённая VAD-реплика → STT → ход кандидата.
// busy-флаг гарантирует один параллельный голосовой ход на соединение (остальные
// реплики теряются — ходовой режим, barge-in вне скоупа).
// preSTT: если распознание уже запущено (pre-STT при первой тишине) и реплика
// не расширялась — ждём его результат (экономит время STT, ~0.7 с на CPU).
func (s *Server) handleVoiceUtterance(ws *wsSession, pcm []byte) {
	defer ws.busy.Store(false)
	if s.voice == nil {
		return
	}
	snap, err := s.engine.Snapshot(ws.id)
	if err != nil || snap.Stage != models.StageVoice || snap.Status != models.StatusActive {
		return // голосовой конвейер работает только на активной voice-стадии
	}

	// Pre-STT: результат, запущенный до завершения VAD-хвоста.
	var res voicesvc.STTResult
	var ok bool
	if p := ws.takePreSTT(); p != nil && int64(len(pcm)) == ws.preSTTBytes.Load() {
		select {
		case <-p.done:
			if !p.errored {
				res, ok = p.res, true
			}
		case <-ws.ctx.Done():
			return
		}
	}
	if !ok {
		// Обычный STT (pre-STT не было, не завершилось или реплика расширялась).
		res, err = s.voice.STT(ws.ctx, pcm)
		if err != nil {
			s.log.Warn("stt: распознавание не удалось", "session", ws.id, "err", err)
			return
		}
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return // молчание/шум — без хода
	}
	// Хвост диктовки (BUG-20261012-018), batch-путь: реплика, завершённая сразу
	// после «recording off» при активной речи — уже в склеенном буфере клиента.
	if t := ws.recTailDiscardAt.Load(); t != 0 {
		ws.recTailDiscardAt.Store(0)
		if !ws.recording.Load() && time.Since(time.Unix(0, t)) < recTailDiscardGrace {
			s.log.Info("recording: отброшен хвост диктовки (batch-путь)", "session", ws.id, "chars", len(text))
			return
		}
	}
	// Фильтр доверия: STT на шуме/дыхании галлюцинирует с низкой уверенностью.
	// Реальные реплики — conf ≥ 0.7 (замеры). Порог 0.5: ниже — шум, без хода.
	if res.Confidence < 0.5 {
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", ws.id, "conf", res.Confidence, "text", truncateForLog(text, 32))
		return
	}
	// Режим записи (FR-S8): сегмент — в окно записи клиента, ход кандидата НЕ
	// стартует (отправка — только явная, выключением микрофона/«Отправить сейчас»).
	if ws.recording.Load() {
		ws.emitRecSegment(s, text, len(pcm)*1000/32000)
		return
	}
	s.log.Info("stt: реплика кандидата", "session", ws.id, "chars", len(text), "conf", res.Confidence, "pre", ok)
	s.runCandidateTurn(ws, text)
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
			s.engine.SendTo(w.id, map[string]any{"type": "stt_partial", "text": ev.Text})
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
		w.stopTTS()
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

// truncateForLog — безопасное усечение строки для лога (не больше n байт;
// не режет юникод-последовательность; короче n — как есть). Паника text[:32]
// на короткой реплике роняла весь процесс (2026-10-08).
func truncateForLog(s string, n int) string {
	if len(s) <= n {
		return s
	}
	for n > 0 && n < len(s) && s[n]&0xC0 == 0x80 {
		n--
	}
	return s[:n]
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
