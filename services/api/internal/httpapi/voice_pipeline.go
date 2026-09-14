package httpapi

import (
	"encoding/binary"
	"strings"
	"unicode/utf8"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// Пайплайн голосового хода (ADR-002): PCM-кадры кандидата → VAD → voice /stt →
// движок интервьюера (LLM) → voice /tts → бинарные кадры PCM16 по WS.
//
// Ходовой режим (SRS §8): пока ИИ «говорит» (стрим TTS) или конвейер занят,
// входящий PCM не слушается (barge-in — вне скоупа MVP).
// Контракт кадров S→C: 4-байтный заголовок {seq u16 LE, flags u16 LE} + PCM16
// 16 кГц mono; flags 0x01 — последний кадр потока (новые реплики — seq заново с 0).

const (
	ttsChunkBytes = 8000 // 250 мс @ 16 кГц PCM16 (ADR-001: кадры ~250 мс)
	ttsFlagEnd    = 0x01
)

// runCandidateTurn — ход кандидата: событие + transcript, ответ ИИ, ai_text,
// transcript, синтез речи (если voice-сервис доступен).
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
	reply, err := s.interviewer.OnUserUtterance(ctx, ws.id, text)
	if err != nil {
		s.sendInterviewerText(ctx, ws.id, "", err)
		return
	}
	s.engine.SendTo(ws.id, map[string]any{"type": "transcript", "who": "ai", "text": reply})
	s.engine.SendTo(ws.id, map[string]any{"type": "ai_text", "text": reply})
	s.streamAIAudio(ws, reply)
}

// streamAIAudio — TTS ответа ИИ → бинарные кадры {seq,flags}+PCM16 (через engine.SendBinary).
// Стриминг по предложениям (бэклог TTS-стриминг): текст реплики разбивается на
// предложения, каждое синтезируется отдельно и отправляется сразу (первый звук —
// через ~0.3 с после LLM, а не после синтеза всего ответа). seq сквозной по
// реплике; end-флаг — только на последнем кадре последнего предложения.
func (s *Server) streamAIAudio(ws *wsSession, text string) {
	if s.voice == nil || strings.TrimSpace(text) == "" {
		return
	}
	sentences := splitSentences(text)
	ws.ttsActive.Store(true)
	defer ws.ttsActive.Store(false)

	seq := 0
	for i, sentence := range sentences {
		if ws.ctx.Err() != nil {
			return
		}
		pcm, err := s.voice.TTS(ws.ctx, sentence)
		if err != nil {
			s.log.Warn("tts: синтез предложения не удался", "session", ws.id, "sentence", i+1, "err", err)
			// Дegrade: пропускаем предложение, идём дальше (голос не критичен, текст уже есть).
			continue
		}
		isLast := i == len(sentences)-1
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
			if isLast && end == len(pcm) {
				binary.LittleEndian.PutUint16(hdr[2:4], ttsFlagEnd)
			}
			frame = append(frame, hdr[:]...)
			frame = append(frame, pcm[off:end]...)
			s.engine.SendBinary(ws.id, frame)
			seq++
		}
	}
	ws.touch() // реплика ИИ завершена — отсчёт тишины кандидата
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
