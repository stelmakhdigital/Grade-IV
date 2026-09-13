package httpapi

import (
	"encoding/binary"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
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
func (s *Server) streamAIAudio(ws *wsSession, text string) {
	if s.voice == nil || strings.TrimSpace(text) == "" {
		return
	}
	pcm, err := s.voice.TTS(ws.ctx, text)
	if err != nil {
		s.log.Warn("tts: синтез не удался", "session", ws.id, "err", err)
		return
	}
	ws.ttsActive.Store(true)
	defer ws.ttsActive.Store(false)

	seq := 0
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
		if end == len(pcm) {
			binary.LittleEndian.PutUint16(hdr[2:4], ttsFlagEnd)
		}
		frame = append(frame, hdr[:]...)
		frame = append(frame, pcm[off:end]...)
		s.engine.SendBinary(ws.id, frame)
		seq++
	}
	ws.touch() // реплика ИИ завершена — отсчёт тишины кандидата
}

// handleVoiceUtterance — goroutine: завершённая VAD-реплика → STT → ход кандидата.
// busy-флаг гарантирует один параллельный голосовой ход на соединение (остальные
// реплики теряются — ходовой режим, barge-in вне скоупа).
func (s *Server) handleVoiceUtterance(ws *wsSession, pcm []byte) {
	defer ws.busy.Store(false)
	if s.voice == nil {
		return
	}
	snap, err := s.engine.Snapshot(ws.id)
	if err != nil || snap.Stage != models.StageVoice || snap.Status != models.StatusActive {
		return // голосовой конвейер работает только на активной voice-стадии
	}
	res, err := s.voice.STT(ws.ctx, pcm)
	if err != nil {
		s.log.Warn("stt: распознавание не удалось", "session", ws.id, "err", err)
		return
	}
	text := strings.TrimSpace(res.Text)
	if text == "" {
		return // молчание/шум — без хода
	}
	s.log.Info("stt: реплика кандидата", "session", ws.id, "chars", len(text), "conf", res.Confidence)
	s.runCandidateTurn(ws, text)
}

// feedVAD — бинарный кадр PCM из readLoop: VAD; по завершённой реплике — конвейер.
func (s *Server) feedVAD(ws *wsSession, pcm []byte) {
	// Пока ИИ говорит или ход занят — не слушаем (turn-taking, ADR-002).
	if ws.ttsActive.Load() || ws.busy.Load() {
		return
	}
	utterance, done := ws.vad.Feed(pcm)
	if !done {
		return
	}
	if !ws.busy.CompareAndSwap(false, true) {
		return // на границе занят — реплика теряется (допустимо в ходовом режиме)
	}
	s.log.Debug("vad: реплика завершена", "session", ws.id, "bytes", len(utterance))
	go s.handleVoiceUtterance(ws, utterance)
}
