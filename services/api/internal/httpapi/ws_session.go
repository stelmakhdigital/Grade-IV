// Состояние WS-соединения: struct wsSession и его аксессоры (stt/vad-стримы,
// реестр Server, TTS-стопы, pre-STT). Отдельный файл: состояние соединения
// отдельно от протокола (ws.go), ui-событий (ws_ui.go) и стадии (ws_livecode.go).

package httpapi

import (
	"context"
	"math"
	"sync"
	"sync/atomic"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/vad"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/voicesvc"
)

// wsSession — состояние WS-соединения сессии: nudge по молчанию, VAD и
// голосовой пайплайн (ADR-002). Пишет в conn только через engine (одно писательство).
type wsSession struct {
	id         int64
	ctx        context.Context // контекст соединения (отмена при обрыве)
	speaker    string          // F1: голос интервьюера (из сессии) — в контекст TTS
	lastActive int64           // unixnano
	lastPCM    int64           // unixnano последнего PCM-кадра от микрофона

	// nudge-лимиты: не чаще раза в 15 с, максимум 3 подряд (сброс при
	// активности кандидата) — без лимита бесконечный поток nudge затапливает
	// контекст LLM и UI («быстро сменяющийся текст»).
	lastNudgeAt atomic.Int64 // unixnano
	nudgeCount  atomic.Int32
	vad         *vad.Detector
	busy        atomic.Bool // голосовой ход занят (один параллельный, turn-taking)
	// Отложенные реплики стрим-пути (V1): финал STT, пришедший пока занят ход
	// (busy — LLM-фаза), не бросается, а накапливается; после хода идёт следом.
	// (handleSTTStreamFinal/runStreamTurn)
	sttPendingMu sync.Mutex
	sttPending   string
	ttsActive    atomic.Bool // ИИ говорит (стрим TTS) — gate barge-in
	// TTS-поток (barge-in): stop-канал текущего стрима; nil — нет активного.
	// Шлющий кадры (paceFrames/streamAIAudio) его наблюдает: closed — стрим
	// остановлен (кандидат прервал речь ИИ).
	ttsMu   sync.Mutex
	ttsStop chan struct{}
	// sessStopped — session-level стоп TTS (FR-S7, раунд 2): ставится при
	// pause/finish, сбрасывается при resume. Все точки, отправляющие TTS-кадры
	// (paceFrames/streamAIAudio/pushTTSJob/диспетчер/beginTTS), его проверяют:
	// после установки новые кадры не уходят, текущий стрим завершается end-кадром
	// (если кадры уже уходили) + WS tts_stop. Закрывает гонку «пауза в LLM-фазе»:
	// beginTTS при флаге возвращает УЖЕ ЗАКРЫТЫЙ канал — стрим не стартует.
	sessStopped atomic.Bool // pause/finish: TTS-кадры не шлём (сброс — resume)
	// Режим записи (FR-S8, ADR-009): клиент включил микрофон — сервис слушает
	// и распознаёт налёт (STT-стриминг), но транскрипт-сегменты НЕ становятся
	// ходами кандидата (AI-ход не стартует) — отправляются как stt_segment в
	// клиентское окно записи. Явная отправка (выключение микрофона / «Отправить
	// сейчас») — клиент шлёт recording off + utterance (WS-порядок гарантирован).
	recording   atomic.Bool
	recSpeechMS atomic.Int64 // суммарная речь (мс) с начала записи (критерий «< 1.5 с»)
	// Pre-STT (voice_pipeline.go): предварительное распознавание при первой
	// тишине — перекрывает VAD-хвост, экономит время STT.
	preSTTActive atomic.Bool  // pre-STT запущен (на текущий буфер)
	preSTTBytes  atomic.Int64 // длина буфера на момент запуска (валидность)
	preSTTMu     sync.Mutex
	preSTT       *preSTTResult

	// Стриминговый STT (ADR-007): voice /stt/stream — PCM-кадры → VAD/STT на
	// стороне voice (Silero): state/partial/final. При недоступности voice
	// (sttStreamDown) — деградация на batch-путь (энергетический VAD + /stt).
	sttStreamMu    sync.Mutex
	sttStream      *voicesvc.STTStream
	sttStreamDown  atomic.Bool  // voice /stt/stream недоступен → следующий уровень
	fallbackWarned atomic.Bool  // Warn + метрика — однократно
	sttSpeechSince atomic.Int64 // unix-nano начала текущего speech-сегмента (state=true); 0 — тишина
	sttBarged      atomic.Bool  // barge-in уже сработал на текущем TTS-стриме (reset в beginTTS)
	// Хвост диктовки (BUG-20261012-018): «recording off» пришёл, пока кандидат ещё
	// говорил (VAD в речи или final уже считается в voice) — следующий final это
	// «хвост диктовки», уже вошедший в клиентский склеенный буфер (последний
	// partial): из него НЕ строим отдельное сообщение кандидата (раньше хвост
	// «отправлялся сам как будто в следующем сообщении»). Одноразовый: расходует
	// первый пришедший final (в пределах recTailDiscardGrace).
	recTailDiscardAt atomic.Int64 // unixnano off-а при активной речи; 0 — хвоста нет
	// Стрим VAD: state=false получен, final ещё считается в voice (STT) —
	// финал в пути (хвост диктовки может прийти после «recording off»).
	sttFinalPending atomic.Bool

	// Silero VAD-стрим (ADR-002, поправка 2026-10-09): voice /vad/stream —
	// 2-й уровень цепочки деградации (/stt/stream → /vad/stream → energy VAD).
	vadStreamMu       sync.Mutex
	vadStream         *voicesvc.VADStream
	vadStreamDown     atomic.Bool // voice /vad/stream недоступен → energy-путь
	vadFallbackWarned atomic.Bool // Warn + метрика — однократно
	sileroSpeech      atomic.Bool // кандидат говорит по Silero-пути (speech_start…utterance)
}

// sttStreamFor — ленивое создание стрим-клиента (voice /stt/stream).
// nil — voice не подключён (batch-режим: VOICE_URL пуст).
func (w *wsSession) sttStreamFor(s *Server) *voicesvc.STTStream {
	if s.voice == nil || s.cfg.VoiceURL == "" {
		return nil
	}
	w.sttStreamMu.Lock()
	defer w.sttStreamMu.Unlock()
	if w.sttStream == nil {
		w.sttStream = s.voice.NewSTTStream(func(ev voicesvc.StreamEvent) {
			w.onSTTStreamEvent(s, ev)
		})
	}
	return w.sttStream
}

// closeSTTStream — остановка стрим-клиента при отключении клиента.
func (w *wsSession) closeSTTStream() {
	w.sttStreamMu.Lock()
	st := w.sttStream
	w.sttStream = nil
	w.sttStreamMu.Unlock()
	if st != nil {
		st.Close()
	}
	// spec-14/A9: стрим закрыт — флаги речи в пути сбрасываем (final уже не придёт).
	w.sttSpeechSince.Store(0)
	w.sttFinalPending.Store(false)
}

// closeVADStream — остановка VAD-стрим-клиента при отключении клиента.
func (w *wsSession) closeVADStream() {
	w.vadStreamMu.Lock()
	vs := w.vadStream
	w.vadStream = nil
	w.vadStreamMu.Unlock()
	if vs != nil {
		vs.Close()
	}
	// spec-14/A9: стрим закрыт — речь в пути не завершится сама.
	w.sileroSpeech.Store(false)
}

// vadStreamFor — ленивое создание VAD-стрим-клиента (voice /vad/stream).
// nil — voice не подключён (energy-режим: VOICE_URL пуст).
func (w *wsSession) vadStreamFor(s *Server) *voicesvc.VADStream {
	if s.voice == nil || s.cfg.VoiceURL == "" {
		return nil
	}
	w.vadStreamMu.Lock()
	defer w.vadStreamMu.Unlock()
	if w.vadStream == nil {
		w.vadStream = s.voice.NewVADStream(func(ev voicesvc.VADEvent) {
			w.onVADStreamEvent(s, ev)
		})
	}
	return w.vadStream
}

// isSpeaking — кандидат говорит по batch-путям (energy VAD last-resort или
// Silero VAD /vad/stream). Nudge не шлём, пока кандидат говорит (см. nudgeLoop).
// Стриминговый путь /stt/stream отслеживается отдельно (sttSpeechSince).
func (w *wsSession) isSpeaking() bool {
	return w.vad.InSpeech() || w.sileroSpeech.Load()
}
func (w *wsSession) touch() { atomic.StoreInt64(&w.lastActive, time.Now().UnixNano()) }

// wsRegister — регистрация живого WS-соединения в реестре Server (1 сессия — 1 соединение).
func (s *Server) wsRegister(id int64, ws *wsSession) {
	s.wsMu.Lock()
	s.wsSessions[id] = ws
	s.wsMu.Unlock()
}

// wsUnregister — снятие WS-соединения из реестра.
func (s *Server) wsUnregister(id int64) {
	s.wsMu.Lock()
	delete(s.wsSessions, id)
	s.wsMu.Unlock()
}

// stopTTSOnAction — pause/finish (FR-S7, раунд 2): session-level стоп TTS.
// (1) ставит sessStopped — все шлющие TTS-кадры (paceFrames/streamAIAudio/
// pushTTSJob/диспетчер/beginTTS) его проверяют: новые кадры не уходят, beginTTS
// вернёт уже закрытый канал (гонка «пауза в LLM-фазе» закрыта);
// (2) останавливает активный стрим barge-in-подобно (stop-канал: стрим
// завершается end-кадром, воркеры синтеза не начинают недиспетчеризованные
// предложения);
// (3) WS tts_stop (клиент останавливает воспроизведение) — всегда, даже если
// активного стрима нет (TTS ещё не начался, но ход уже прошёл статус-гард).
// Вызывается после успешного engine.Pause/Finish; при resume флаг сбрасывается.
func (s *Server) stopTTSOnAction(id int64, action string) {
	s.wsMu.Lock()
	ws := s.wsSessions[id]
	s.wsMu.Unlock()
	if ws == nil {
		return
	}
	ws.sessStopped.Store(true)
	wasActive := ws.stopTTS()
	s.log.Info(action+": session-level стоп TTS установлен", "session", id, "active_stream", wasActive)
	s.engine.SendTo(id, map[string]any{"type": "tts_stop"})
}

// wsSessionFor — живое WS-соединение сессии из реестра (nil — не подключено).
func (s *Server) wsSessionFor(id int64) *wsSession {
	s.wsMu.Lock()
	ws := s.wsSessions[id]
	s.wsMu.Unlock()
	return ws
}

// resetSessionStopped — resume (FR-S7, раунд 2): сброс session-level стопа TTS.
// nil-безопасно (клиент не подключён — нового соединения будет своё wsSession).
func (w *wsSession) resetSessionStopped() {
	if w != nil {
		w.sessStopped.Store(false)
	}
}

// beginTTS — регистрация начала TTS-стрима (заменяет текущий, если был):
// возвращает stop-канал, который наблюдает шлющий кадры; ставит ttsActive.
// Session-level стоп (sessStopped, pause/finish): возвращает УЖЕ ЗАКРЫТЫЙ
// канал и стрим НЕ регистрирует — кадров не будет (гонка «пауза в LLM-фазе»):
// paceFrames/streamAIAudio видят closed-stop сразу и уходят без кадров
// (tts_stop клиенту уже отправлен в stopTTSOnAction).
func (w *wsSession) beginTTS() <-chan struct{} {
	w.ttsMu.Lock()
	defer w.ttsMu.Unlock()
	if w.sessStopped.Load() {
		c := make(chan struct{})
		close(c)
		return c
	}
	if w.ttsStop != nil {
		close(w.ttsStop)
	}
	w.ttsStop = make(chan struct{})
	w.ttsActive.Store(true)
	w.sttBarged.Store(false)
	return w.ttsStop
}

// endTTS — нормальное завершение TTS-стрима (после end-кадра): сбрасывает
// ttsActive, только если стрим последний (не заменён более новым).
func (w *wsSession) endTTS(stop <-chan struct{}) {
	w.ttsMu.Lock()
	defer w.ttsMu.Unlock()
	if w.ttsStop == stop {
		w.ttsStop = nil
		w.ttsActive.Store(false)
	}
}

// stopTTS — barge-in: остановка текущего TTS-стрима (stop-канал закрыт,
// ttsActive сброшен). true — был активный стрим.
func (w *wsSession) stopTTS() bool {
	w.ttsMu.Lock()
	defer w.ttsMu.Unlock()
	if w.ttsStop == nil {
		return false
	}
	close(w.ttsStop)
	w.ttsStop = nil
	w.ttsActive.Store(false)
	return true
}

// stopped — закрыт ли stop-канал TTS-стрима.
func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// lastCandActivity — когда кандидат последний раз был активен (PCM от
// микрофона). Nudge-отсчёт ведётся от НЕЙ, а не от любой активности:
// иначе nudge-цикл сам обновляет «активность» → бесконечный поток nudge
// (текст «быстро сменяется»).
func (w *wsSession) lastCandActivity() int64 {
	if n := atomic.LoadInt64(&w.lastPCM); n > 0 {
		return n
	}
	return atomic.LoadInt64(&w.lastActive)
}

// setPreSTT / takePreSTT — доступ к pre-STT результату (mutex: гоутрутин
// записывает, ходовой конвейер читает).
func (w *wsSession) setPreSTT(p *preSTTResult) {
	w.preSTTMu.Lock()
	defer w.preSTTMu.Unlock()
	w.preSTT = p
}
func (w *wsSession) takePreSTT() *preSTTResult {
	w.preSTTMu.Lock()
	defer w.preSTTMu.Unlock()
	p := w.preSTT
	w.preSTT = nil
	return p
}
func (w *wsSession) silentS() int {
	return int(time.Since(time.Unix(0, w.lastCandActivity())).Seconds())
}

// pcm16RMS — RMS уровня PCM16 mono (0..32767); используется в тестах голосового контура.
func pcm16RMS(pcm []byte) int {
	if len(pcm) < 2 {
		return 0
	}
	var sum float64
	n := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := float64(int16(pcm[i]) | int16(pcm[i+1])<<8)
		sum += v * v
		n++
	}
	if n == 0 {
		return 0
	}
	return int(math.Sqrt(sum / float64(n)))
}
