package httpapi

import (
	"strings"
	"time"

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

func splitForTTS(text string) []string { return voiceorchestrator.SplitForTTS(text) }

func ttsGap(prev string) int { return voiceorchestrator.TTSGap(prev) }

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
