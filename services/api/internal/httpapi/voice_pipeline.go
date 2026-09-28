package httpapi

import (
	"encoding/binary"
	"errors"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Пайплайн голосового хода (ADR-002): PCM-кадры кандидата → VAD → voice /stt →
// движок интервьюера (LLM, SSE-стрим) → voice /tts (по предложениям, до-стриминг)
// → бинарные кадры PCM16 по WS с pacing (не быстрее real-time).
//
// Ходовой режим (SRS §8): пока ИИ «говорит» (стрим TTS) или конвейер занят,
// входящий PCM не слушается (barge-in — вне скоупа MVP).
// Контракт кадров S→C: 4-байтный заголовок {seq u16 LE, flags u16 LE} + PCM16
// 16 кГц mono; flags 0x01 — последний кадр потока (новые реплики — seq заново с 0).

const (
	ttsChunkBytes = 8000 // 250 мс @ 16 кГц PCM16 (ADR-001: кадры ~250 мс)
	ttsFlagEnd    = 0x01
	// frameDur — pacing: один кадр = 250 мс аудио, кадры уходят не чаще
	// одного за 250 мс (реальное время).
	frameDur = 250 * time.Millisecond
)

// runCandidateTurn — ход кандидата: событие + transcript, ответ ИИ (SSE-стрим),
// ai_text (единым сообщением по завершении LLM-текста), TTS-кадры с pacing.
// Вызывается из readLoop (текстовый utterance) и из goroutine голосового STT.
func (s *Server) runCandidateTurn(ws *wsSession, text string) {
	ctx := ws.ctx
	sess, err := s.sessions.Get(ctx, ws.id)
	if err != nil || sess.Status != models.StatusActive {
		return // сессия завершилась во время хода
	}
	if _, err := s.eventData(ctx, ws.id, "user_utterance", map[string]any{"text": text}); err == nil {
		s.engine.SendTo(ws.id, map[string]any{"type": "transcript", "who": "user", "text": text})
	}
	ch, err := s.interviewer.OnUserUtteranceStream(ctx, ws.id, text)
	if err != nil {
		metrics.LLMStreamErrors.Inc(nil)
		metrics.TurnsTotal.Inc(metrics.ResultLabel("error"))
		s.sendInterviewerText(ctx, ws.id, "", err)
		return
	}
	s.streamCandidateTurn(ws, ch)
}

// streamCandidateTurn — конвейер хода: LLM-токены → полные предложения (логика
// splitSentences) → TTS по мере готовности (до-стриминг: LLM ещё пишет, а TTS
// уже синтезирует) → кадры в WS с pacing. ai_text/transcript — одним
// сообщением, когда весь LLM-текст собран (frontend использует ai_text как
// финальный текст, не инкрементальный).
func (s *Server) streamCandidateTurn(ws *wsSession, deltas <-chan string) {
	start := time.Now() // turn_start (после STT)

	sentences := make(chan string, 8)
	fullText := make(chan string, 1)
	go splitDeltas(start, deltas, sentences, fullText)

	voiceOn := s.voice != nil
	var turnDone chan struct{} // завершение аудио-конвейера (pacing)
	var frames chan []byte     // PCM-кадры (≤ ttsChunkBytes) из TTS-воркера
	if voiceOn {
		ws.ttsActive.Store(true) // gate микрофона: сброс строго после end-кадра
		frames = make(chan []byte, 64)
		go func() {
			defer close(frames)
			silence := make([]byte, ttsChunkBytes)
			prev := "" // предыдущее предложение (пауза перед текущим)
			for sentence := range sentences {
				if prev != "" {
					gap := 1
					if strings.HasSuffix(prev, "?") || strings.HasSuffix(prev, "!") {
						gap = 2
					}
					for g := 0; g < gap; g++ {
						frames <- silence
					}
				}
				prev = sentence
				pcm, err := s.voice.TTS(ws.ctx, prepareTTS(sentence))
				if err != nil {
					metrics.TTSErrors.Inc(nil)
					s.log.Warn("tts: синтез предложения не удался", "session", ws.id, "err", err)
					continue // Degrade: пропускаем (голос не критичен, текст уже есть)
				}
				for off := 0; off < len(pcm); off += ttsChunkBytes {
					end := off + ttsChunkBytes
					if end > len(pcm) {
						end = len(pcm)
					}
					frames <- pcm[off:end]
				}
			}
		}()
		// Pacing параллельно LLM-стриму: кадры уходят, пока LLM ещё пишет.
		turnDone = make(chan struct{})
		go func() {
			defer close(turnDone)
			defer ws.ttsActive.Store(false) // строго после end-кадра
			_, _, ended := s.paceFrames(ws, start, frames)
			if ended {
				metrics.TurnStage.Observe(metrics.StageLabel("turn_end"), time.Since(start).Seconds())
				metrics.TurnsTotal.Inc(metrics.ResultLabel("ok"))
			} else {
				metrics.TurnsTotal.Inc(metrics.ResultLabel("no_audio")) // ctx-отмена/без кадров
			}
		}()
	} else {
		go func() {
			for range sentences {
			}
		}() // voice нет — просто сливаем
	}

	full := <-fullText // LLM-текст собран (стрим завершён)
	full = strings.TrimSpace(full)
	if full == "" {
		// Деградация: LLM не дала текста (оборванный стрим) — fallback как при ошибке.
		metrics.TurnsTotal.Inc(metrics.ResultLabel("error"))
		s.log.Warn("interviewer: LLM-стрим вернул пустой ответ", "session", ws.id)
		s.sendInterviewerText(ws.ctx, ws.id, "", errEmptyStream)
	} else {
		s.engine.SendTo(ws.id, map[string]any{"type": "transcript", "who": "ai", "text": full})
		s.engine.SendTo(ws.id, map[string]any{"type": "ai_text", "text": full})
	}

	if turnDone != nil {
		<-turnDone // ждём конца аудио (end-кадр ушёл, ttsActive сброшен)
	} else {
		// Без voice: ход завершён сбором текста.
		metrics.TurnStage.Observe(metrics.StageLabel("turn_end"), time.Since(start).Seconds())
		metrics.TurnsTotal.Inc(metrics.ResultLabel("ok"))
	}
}

var errEmptyStream = errors.New("LLM-стрим вернул пустой ответ")

// splitDeltas — LLM-стрим → полные предложения (TTS) + полный текст (ai_text).
// Предложение считается готовым, когда по логике splitSentences после него
// начинается новое; остаток — по завершении стрима.
func splitDeltas(start time.Time, deltas <-chan string, sentences chan<- string, fullText chan<- string) {
	var full, pending string
	for d := range deltas {
		if full == "" {
			metrics.TurnStage.Observe(metrics.StageLabel("llm_first_token"), time.Since(start).Seconds())
		}
		full += d
		pending += d
		parts := splitSentences(pending)
		if len(parts) > 1 {
			for _, p := range parts[:len(parts)-1] {
				sentences <- p
			}
			pending = parts[len(parts)-1]
		}
	}
	if p := strings.TrimSpace(pending); p != "" {
		sentences <- p
	}
	close(sentences)
	fullText <- full
}

// paceFrames — очередь кадров → WS: не быстрее одного кадра за 250 мс
// (1 кадр = 250 мс аудио), очередь опустошается к концу стрима; end-флаг —
// на самом последнем кадре. Возвращает время первого/последнего кадра и
// завершён ли поток end-кадром (ctx-отмена — нет).
func (s *Server) paceFrames(ws *wsSession, turnStart time.Time, frames chan []byte) (time.Time, time.Time, bool) {
	var seq uint16
	silence := make([]byte, ttsChunkBytes)
	ticker := time.NewTicker(25 * time.Millisecond) // разрешение pacing-списка
	defer ticker.Stop()
	var hold []byte
	var queue [][]byte     // накопленные кадры (LLM/TTS обгоняют pacing)
	var nextSend time.Time // zero = можно отправлять сразу (первый кадр)
	var firstAt, endAt time.Time
	sentAny := false
	endPending := false
	for {
		if ws.ctx.Err() != nil {
			return firstAt, endAt, false
		}
		if len(queue) > 0 && !nextSend.After(time.Now()) {
			hold, queue = queue[0], queue[1:]
			sendEnd := endPending
			frame := make([]byte, 0, 4+len(hold))
			var hdr [4]byte
			binary.LittleEndian.PutUint16(hdr[0:2], seq)
			if sendEnd {
				binary.LittleEndian.PutUint16(hdr[2:4], ttsFlagEnd)
			}
			frame = append(frame, hdr[:]...)
			frame = append(frame, hold...)
			s.engine.SendBinary(ws.id, frame)
			seq++
			now := time.Now()
			if !sentAny {
				firstAt = now
				metrics.TurnStage.Observe(metrics.StageLabel("tts_first_frame"), now.Sub(turnStart).Seconds())
			}
			sentAny = true
			if sendEnd {
				endAt = now
				return firstAt, endAt, true
			}
			hold = nil
			nextSend = now.Add(frameDur)
			continue
		}
		select {
		case <-ticker.C:
		case pcm, ok := <-frames:
			if ok {
				queue = append(queue, pcm)
				continue
			}
			if sentAny {
				queue = append(queue, silence)
				endPending = true // финальный end-кадр (тишина 250 мс)
				continue
			}
			return firstAt, endAt, sentAny // кадров не было (все TTS-ошибки) — stop не нужен
		}
	}
}

// streamAIAudio — TTS ответа ИИ → бинарные кадры {seq,flags}+PCM16 (через engine.SendBinary).
// Стриминг по предложениям (бэклог TTS-стриминг): текст реплики разбивается на
// предложения, каждое синтезируется отдельно и отправляется сразу (первый звук —
// через ~0.3 с после LLM, а не после синтеза всего ответа). seq сквозной по
// реплике; end-флаг — только на последнем кадре всего потока.
//
// Подготовка для TTS: латинские термины транслитерируются (prepareTTS) —
// Silero v5 не озвучивает латиницу («Go» -> «гоу»); текст в UI остаётся
// оригинальным. Между предложениями вставляется тишина (пауза): 250 мс
// после «.»/«...», 500 мс после «?»/«!» — чтобы реплики не сливались.
func (s *Server) streamAIAudio(ws *wsSession, text string) {
	if s.voice == nil || strings.TrimSpace(text) == "" {
		return
	}
	sentences := splitSentences(text)
	ws.ttsActive.Store(true)
	defer ws.ttsActive.Store(false)

	seq := 0
	sentAny := false                       // уходил ли хоть один кадр (для финального end-кадра)
	silence := make([]byte, ttsChunkBytes) // 250 мс тишины
	for i, sentence := range sentences {
		if ws.ctx.Err() != nil {
			return
		}
		pcm, err := s.voice.TTS(ws.ctx, prepareTTS(sentence))
		if err != nil {
			s.log.Warn("tts: синтез предложения не удался", "session", ws.id, "sentence", i+1, "err", err)
			// Degrade: пропускаем предложение, идём дальше (голос не критичен, текст уже есть).
			continue
		}
		for off := 0; off < len(pcm); off += ttsChunkBytes {
			if ws.ctx.Err() != nil {
				return
			}
			end := off + ttsChunkBytes
			if end > len(pcm) {
				end = len(pcm)
			}
			frame := make([]byte, 0, 4+end-off)
			var hdr [4]byte
			binary.LittleEndian.PutUint16(hdr[0:2], uint16(seq))
			frame = append(frame, hdr[:]...)
			frame = append(frame, pcm[off:end]...)
			s.engine.SendBinary(ws.id, frame)
			seq++
			sentAny = true
		}
		// Пауза между предложениями (не ставим после последнего).
		if i < len(sentences)-1 {
			gap := 1
			if strings.HasSuffix(sentence, "?") || strings.HasSuffix(sentence, "!") {
				gap = 2
			}
			for g := 0; g < gap; g++ {
				if ws.ctx.Err() != nil {
					return
				}
				frame := make([]byte, 0, 4+len(silence))
				var hdr [4]byte
				binary.LittleEndian.PutUint16(hdr[0:2], uint16(seq))
				frame = append(frame, hdr[:]...)
				frame = append(frame, silence...)
				s.engine.SendBinary(ws.id, frame)
				seq++
			}
		}
	}
	// Финальный кадр с end-флагом: чистый хвост (тишина 250 мс) и гарантия
	// остановки плеера. Только если реплики реально озвучивались (хотя бы
	// один кадр ушёл) — иначе потока кадров не было и stop не нужен.
	if sentAny && ws.ctx.Err() == nil {
		frame := make([]byte, 0, 4+len(silence))
		var hdr [4]byte
		binary.LittleEndian.PutUint16(hdr[0:2], uint16(seq))
		binary.LittleEndian.PutUint16(hdr[2:4], ttsFlagEnd)
		frame = append(frame, hdr[:]...)
		frame = append(frame, silence...)
		s.engine.SendBinary(ws.id, frame)
	}
}

// splitSentences — разбивка текста на предложения для TTS-стриминга:
// «!»/«?» — всегда (дальше пробел/конец); «.» — только перед ЗАГЛАВНОЙ буквой
// (лат/кирл) или в конце текста — русские аббревиатуры («т.д.», «т.п.») с
// малой буквой не дробят; многоточие «...» — разрыв.
func splitSentences(text string) []string {
	text = strings.TrimSpace(text)
	if text == "" {
		return nil
	}
	var parts []string
	start := 0
	flush := func(end int) {
		part := strings.TrimSpace(text[start:end])
		if part != "" {
			parts = append(parts, part)
		}
		start = end
	}
	for i := 0; i < len(text); i++ {
		// Многоточие «...» — разрыв.
		if i+2 < len(text) && text[i] == '.' && text[i+1] == '.' && text[i+2] == '.' {
			flush(i + 3)
			i += 2
			continue
		}
		boundary := false
		switch text[i] {
		case '!', '?':
			boundary = true
		case '.':
			// «.» — только перед заглавной (новое предложение) или в конце.
			// Пропускаем пробелы после точки.
			j := i + 1
			for j < len(text) && (text[j] == ' ' || text[j] == '\n' || text[j] == '\t') {
				j++
			}
			boundary = j >= len(text) || isUpperNext(text, j)
		}
		if boundary && (i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' || text[i+1] == '\t') {
			flush(i + 1)
		}
	}
	flush(len(text))
	return parts
}

// isUpperNext — следующий после индекса i символ — заглавная (лат/кирл).
func isUpperNext(text string, i int) bool {
	if i >= len(text) {
		return false
	}
	r, size := utf8.DecodeRuneInString(text[i:])
	if size <= 1 {
		return false // уже обработано ASCII-путём
	}
	return (r >= 'A' && r <= 'Z') || (r >= 0x0410 && r <= 0x042F)
}

// preSTTResult — предварительное распознавание (pre-STT): запущено на
// текущем буфере реплики при первой тишине (PreSilence), перекрывает остаток
// VAD-хвоста. Валидно, если реплика не расширялась после запуска.
type preSTTResult struct {
	res     voicesvc.STTResult
	errored bool
	done    chan struct{}
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
	// Фильтр доверия: STT на шуме/дыхании галлюцинирует с низкой уверенностью.
	// Реальные реплики — conf ≥ 0.7 (замеры). Порог 0.5: ниже — шум, без хода.
	if res.Confidence < 0.5 {
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", ws.id, "conf", res.Confidence, "text", text[:32])
		return
	}
	s.log.Info("stt: реплика кандидата", "session", ws.id, "chars", len(text), "conf", res.Confidence, "pre", ok)
	s.runCandidateTurn(ws, text)
}

// feedVAD — бинарный кадр PCM из readLoop: VAD; по завершённой реплике — конвейер.
// Pre-STT: при первой тишине после речи (vad.PreSilence) запускаем распознавание
// на текущем буфере реплики — оно перекрывает остаток VAD-хвоста и экономит
// время STT (~0.7 с на CPU). Инвалидация при возобновлении речи (новые speech-
// кадры расширяют буфер → preSTTBytes не совпадёт).
func (s *Server) feedVAD(ws *wsSession, pcm []byte) {
	// Пока ИИ говорит или ход занят — не слушаем (turn-taking, ADR-002).
	if ws.ttsActive.Load() || ws.busy.Load() {
		return
	}
	utterance, done := ws.vad.Feed(pcm)
	if !done {
		// Pre-STT: предварительное распознавание при первой тишине.
		if ws.vad.PreSilence() && ws.preSTTActive.CompareAndSwap(false, true) {
			buf := ws.vad.Utterance()
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
		return
	}
	// Реплика завершена: сбрасываем pre-STT-флаг (если ещё не сброшен).
	ws.preSTTActive.Store(false)
	if !ws.busy.CompareAndSwap(false, true) {
		return // на границе занят — реплика теряется (допустимо в ходовом режиме)
	}
	s.log.Debug("vad: реплика завершена", "session", ws.id, "bytes", len(utterance))
	go s.handleVoiceUtterance(ws, utterance)
}
