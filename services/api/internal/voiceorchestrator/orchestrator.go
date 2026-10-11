package voiceorchestrator

import (
	"context"
	"encoding/binary"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/metrics"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// errEmptyStream — LLM-стрим не вернул текста (оборванный стрим).
var errEmptyStream = errors.New("LLM-стрим вернул пустой ответ")

// stopped — закрыт ли stop-канал (barge-in).
func stopped(stop <-chan struct{}) bool {
	select {
	case <-stop:
		return true
	default:
		return false
	}
}

// Conn — подключение (WS-сессия), над которым работает Orchestrator. Реализует
// httpapi.wsSession. Держит только то, что нужно голосовому конвейеру (состояние
// TTS/pause/контекст/id) — transport (WebSocket) остаётся в httpapi.
type Conn interface {
	// ID — id сессии (для engine.SendBinary/SendTo и логов/метрик).
	ID() int64
	// Ctx — контекст подключения (отмена = конец сессии).
	Ctx() context.Context
	// SessStopped — пауза/завершение сессии (FR-S7): конвейер останавливается.
	SessStopped() bool
	// BeginTTS — начать TTS-стрим: возвращает stop-канал (закрывается при barge-in).
	BeginTTS() <-chan struct{}
	// EndTTS — завершить TTS-стрим (сброс ttsActive после end-кадра).
	EndTTS(stop <-chan struct{})
}

// Engine — движок сессий (транспорт событий/кадров к клиенту).
type Engine interface {
	Snapshot(id int64) (*session.Snapshot, error)
	SendTo(id int64, v any)
	SendBinary(id int64, data []byte)
}

// Voice — TTS-синтез (voicesvc). Передаётся как параметр методов (не кэшируется):
// httpapi может подменить s.voice после конструирования Server (тесты).
type Voice interface {
	TTS(ctx context.Context, text string) ([]byte, error)
}

// Interviewer — LLM-интервьюер (стрим ответа на реплику кандидата).
type Interviewer interface {
	OnUserUtteranceStream(ctx context.Context, sessionID int64, text string) (<-chan string, error)
}

// Deps — зависимости Orchestrator, вводимые через DI (конструктор). Стабильные
// (не подменяются после New): Engine, Interviewer, Log, колбэки событий/текста.
type Deps struct {
	Engine      Engine
	Interviewer Interviewer
	Log         *slog.Logger
	// EventData — запись события в журнал сессии (httpapi.(*Server).eventData).
	EventData func(ctx context.Context, sessionID int64, kind string, data map[string]any) (int, error)
	// SendInterviewerText — отправка результата/ошибки LLM в UI (httpapi.(*Server).sendInterviewerText).
	SendInterviewerText func(ctx context.Context, id int64, result string, err error)
}

// Orchestrator — голосовой конвейер хода VAD→STT→LLM→TTS (LLM-фаза + TTS).
// Выведен из httpapi (A1): раньше это были методы *Server, завязанные на
// wsSession. Логика та же — только зависимости через DI, state через Conn.
type Orchestrator struct {
	engine              Engine
	interviewer         Interviewer
	log                 *slog.Logger
	eventData           func(ctx context.Context, sessionID int64, kind string, data map[string]any) (int, error)
	sendInterviewerText func(ctx context.Context, id int64, result string, err error)
}

// New — создаёт Orchestrator с явными зависимостями (DI).
func New(d Deps) *Orchestrator {
	lg := d.Log
	if lg == nil {
		lg = slog.New(slog.DiscardHandler)
	}
	return &Orchestrator{
		engine:              d.Engine,
		interviewer:         d.Interviewer,
		log:                 lg,
		eventData:           d.EventData,
		sendInterviewerText: d.SendInterviewerText,
	}
}

// RunCandidateTurn — ход кандидата: событие + transcript, ответ ИИ (LLM-стрим),
// ai_text (по завершении LLM-текста), TTS-кадры с pacing.
// Вызывается из readLoop (текстовый utterance) и из goroutine голосового STT.
// voice — текущий TTS-клиент (ниль = без озвучки, только текст).
func (o *Orchestrator) RunCandidateTurn(c Conn, voice Voice, text string) {
	ctx := c.Ctx()
	// Статус — из движка (in-memory): paused/завершённая сессия не начинает ход (FR-S7).
	if snap, err := o.engine.Snapshot(c.ID()); err != nil || snap.Status != models.StatusActive {
		return
	}
	if _, err := o.eventData(ctx, c.ID(), "user_utterance", map[string]any{"text": text}); err == nil {
		o.engine.SendTo(c.ID(), map[string]any{"type": "transcript", "who": "user", "text": text})
	}
	ch, err := o.interviewer.OnUserUtteranceStream(ctx, c.ID(), text)
	if err != nil {
		metrics.LLMStreamErrors.Inc(nil)
		metrics.TurnsTotal.Inc(metrics.ResultLabel("error"))
		o.sendInterviewerText(ctx, c.ID(), "", err)
		return
	}
	o.StreamCandidateTurn(c, voice, ch)
}

// StreamCandidateTurn — конвейер хода: LLM-токены → полные предложения (SplitForTTS)
// → TTS по мере готовности (до-стриминг) → кадры в WS с pacing. ai_text/transcript —
// одним сообщением, когда весь LLM-текст собран.
func (o *Orchestrator) StreamCandidateTurn(c Conn, voice Voice, deltas <-chan string) {
	start := time.Now() // turn_start (после STT)

	sentences := make(chan string, 8)
	fullText := make(chan string, 1)
	go o.SplitDeltas(start, deltas, sentences, fullText)

	voiceOn := voice != nil
	var turnDone chan struct{} // завершение аудио-конвейера (pacing)
	var frames chan []byte     // PCM-кадры (≤ ChunkBytes) из TTS-воркера
	var stop <-chan struct{}   // stop-канал TTS-стрима (barge-in)
	if voiceOn {
		stop = c.BeginTTS() // gate микрофона: сброс строго после end-кадра
		frames = make(chan []byte, 64)
		go func() {
			defer close(frames)
			o.ProduceOrderedTTS(c, voice, sentences, frames, stop)
		}()
		// Pacing параллельно LLM-стриму: кадры уходят, пока LLM ещё пишет.
		turnDone = make(chan struct{})
		go func() {
			defer close(turnDone)
			defer c.EndTTS(stop) // строго после end-кадра (или barge-in)
			_, _, ended := o.PaceFrames(c, start, frames, stop)
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
		metrics.TurnsTotal.Inc(metrics.ResultLabel("error"))
		o.log.Warn("interviewer: LLM-стрим вернул пустой ответ", "session", c.ID())
		o.sendInterviewerText(c.Ctx(), c.ID(), "", errEmptyStream)
	} else {
		o.engine.SendTo(c.ID(), map[string]any{"type": "transcript", "who": "ai", "text": full})
		o.engine.SendTo(c.ID(), map[string]any{"type": "ai_text", "text": full})
	}

	if turnDone != nil {
		<-turnDone // ждём конца аудио (end-кадр ушёл, ttsActive сброшен)
	} else {
		// Без voice: ход завершён сбором текста.
		metrics.TurnStage.Observe(metrics.StageLabel("turn_end"), time.Since(start).Seconds())
		metrics.TurnsTotal.Inc(metrics.ResultLabel("ok"))
	}
}

// SplitDeltas — LLM-стрим → готовые части для TTS + полный текст (ai_text).
// Клиуза-уровневая диспетчизация: часть готова, когда по SplitForTTSStreaming
// после неё начинается новая; остаток — по завершении стрима.
func (o *Orchestrator) SplitDeltas(start time.Time, deltas <-chan string, sentences chan<- string, fullText chan<- string) {
	var full, pending string
	for d := range deltas {
		if full == "" {
			metrics.TurnStage.Observe(metrics.StageLabel("llm_first_token"), time.Since(start).Seconds())
		}
		full += d
		pending += d
		parts, tail := SplitForTTSStreaming(pending)
		for _, p := range parts {
			sentences <- p
		}
		pending = tail // открытый хвост (raw: пробелы не теряются)
	}
	if p := strings.TrimSpace(pending); p != "" {
		sentences <- p
	}
	close(sentences)
	fullText <- full
}

// ttsJob — предложение для TTS-синтеза. idx — порядок вывода, gap — число
// тише-кадров перед предложением (по пунктуации предыдущего).
type ttsJob struct {
	idx  int
	gap  int
	text string
}

// ProduceOrderedTTS — параллельный TTS-синтез (пул из TTSParallelism воркеров) с
// сохранением порядка вывода: кадры N (с паузой) уходят в frames до кадров N+1.
// Barge-in (stop закрыт): недиспетчеризованные предложения не начинаются,
// запущенные доводятся до конца, результаты сливаются (без утечки горутины).
func (o *Orchestrator) ProduceOrderedTTS(c Conn, voice Voice, sentences <-chan string, frames chan<- []byte, stop <-chan struct{}) {
	// 1) Диспетчер: читает предложения по порядку, назначает idx/gap, в пул.
	jobs := make(chan ttsJob)
	go func() {
		defer close(jobs)
		prev := ""
		idx := 0
		for sentence := range sentences {
			if stopped(stop) || c.SessStopped() {
				return // barge-in / pause-finish: не начинаем недиспетчеризованные
			}
			gap := TTSGap(prev)
			prev = sentence
			idx++
			jobs <- ttsJob{idx: idx, gap: gap, text: sentence}
		}
	}()

	// 2) Воркеры: параллельный синтез; результаты могут прийти не по порядку.
	type result struct {
		idx int
		gap int
		pcm []byte
		err error
	}
	results := make(chan result, TTSParallelism)
	var wg sync.WaitGroup
	for i := 0; i < TTSParallelism; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := range jobs {
				st := time.Now()
				pcm, err := voice.TTS(c.Ctx(), PrepareTTS(j.text))
				if err != nil {
					metrics.TTSErrors.Inc(nil)
					o.log.Warn("tts: не удалось синтезировать предложение", "session", c.ID(), "err", err)
				} else {
					metrics.TTSSynth.Observe(nil, time.Since(st).Seconds())
				}
				results <- result{idx: j.idx, gap: j.gap, pcm: pcm, err: err}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()

	// 3) Записчик: реордер по idx → frames строго по порядку. Barge-in — только слив.
	nextWanted := 1
	pending := map[int]result{}
	bargedIn := false
	for r := range results {
		pending[r.idx] = r
		for {
			cur, ok := pending[nextWanted]
			if !ok {
				break
			}
			delete(pending, nextWanted)
			nextWanted++
			if bargedIn || cur.err != nil {
				continue // barge-in — только слив; ошибка — skip (лог выше)
			}
			if !o.pushTTSJob(c, frames, stop, cur.gap, cur.pcm) {
				bargedIn = true
			}
		}
	}
}

// pushTTSJob — кадры предложения (тише-пауза + PCM) в frames. select на stop
// (barge-in), SessStopped (pause/finish, FR-S7) и Ctx (конец сессии): при любом
// из них перестаёт слать — false (end-кадр шлёт PaceFrames).
func (o *Orchestrator) pushTTSJob(c Conn, frames chan<- []byte, stop <-chan struct{}, gap int, pcm []byte) bool {
	silence := make([]byte, ChunkBytes)
	send := func(b []byte) bool {
		select {
		case frames <- b:
			return true
		case <-stop:
			return false
		case <-c.Ctx().Done():
			return false
		}
	}
	for g := 0; g < gap; g++ {
		if c.SessStopped() {
			return false
		}
		if !send(silence) {
			return false
		}
	}
	for off := 0; off < len(pcm); off += ChunkBytes {
		if c.SessStopped() {
			return false
		}
		end := off + ChunkBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		if !send(pcm[off:end]) {
			return false
		}
	}
	return true
}

// PaceFrames — очередь кадров → WS: не быстрее одного кадра за 250 мс (1 кадр =
// 250 мс аудио); end-флаг — только на финальном кадре. Возвращает время
// первого/последнего кадра и завершён ли поток end-кадром (ctx-отмена — нет).
func (o *Orchestrator) PaceFrames(c Conn, turnStart time.Time, frames chan []byte, stop <-chan struct{}) (time.Time, time.Time, bool) {
	var seq uint16
	silence := make([]byte, ChunkBytes)
	ticker := time.NewTicker(25 * time.Millisecond) // разрешение pacing-списка
	defer ticker.Stop()
	var queue [][]byte     // накопленные кадры (LLM/TTS обгоняют pacing)
	var nextSend time.Time // zero = можно отправлять сразу (первый кадр)
	var firstAt, endAt time.Time
	sentAny := false
	closed := false      // TTS-воркер завершил поставку кадров
	preBufferFrames := 2 // V2: 2 кадра (500 мс) — пре-буфер клиента
	for {
		if c.Ctx().Err() != nil {
			return firstAt, endAt, false
		}
		if stopped(stop) || c.SessStopped() {
			if sentAny {
				o.SendTTSFrame(c, seq, silence, true)
				endAt = time.Now()
			}
			return firstAt, endAt, sentAny
		}
		now := time.Now()
		// Финальный end-кадр: все кадры получены и отправлены (по pacing-ритму).
		if closed && len(queue) == 0 && !nextSend.After(now) {
			if !sentAny {
				return firstAt, endAt, false // кадров не было (все TTS-ошибки)
			}
			o.SendTTSFrame(c, seq, silence, true)
			return firstAt, now, true
		}
		// Пре-буфер (V2): первый кадр — только когда накоплено ≥ preBufferFrames (или
		// конец потока): just-in-time pacing (250 мс/кадр = 250 мс аудио) без задела
		// → при любом джиттере/дрейфе pacing клиентский буфер пустеет (underrun =
		// рывки/обрывы). Дальше — по pacing-ритму (nextSend).
		if len(queue) > 0 && !nextSend.After(now) && (seq > 0 || closed || len(queue) >= preBufferFrames) {
			hold := queue[0]
			queue = queue[1:]
			frame := make([]byte, 0, 4+len(hold))
			var hdr [4]byte
			binary.LittleEndian.PutUint16(hdr[0:2], seq)
			frame = append(frame, hdr[:]...)
			frame = append(frame, hold...)
			o.engine.SendBinary(c.ID(), frame)
			seq++
			if !sentAny {
				firstAt = now
				metrics.TurnStage.Observe(metrics.StageLabel("tts_first_frame"), now.Sub(turnStart).Seconds())
			}
			sentAny = true
			nextSend = now.Add(FrameDur)
			continue
		}
		if !closed {
			select {
			case <-ticker.C:
			case pcm, ok := <-frames:
				if !ok {
					closed = true
					continue
				}
				queue = append(queue, pcm)
				continue
			}
		} else {
			<-ticker.C // очередь недополнена/ограничена pacing — ждём такта
		}
	}
}

// SendTTSFrame — бинарный кадр TTS {seq u16 LE, flags u16 LE}+PCM16 (ADR-001).
func (o *Orchestrator) SendTTSFrame(c Conn, seq uint16, pcm []byte, end bool) {
	frame := make([]byte, 0, 4+len(pcm))
	var hdr [4]byte
	binary.LittleEndian.PutUint16(hdr[0:2], seq)
	if end {
		binary.LittleEndian.PutUint16(hdr[2:4], FlagEnd)
	}
	frame = append(frame, hdr[:]...)
	frame = append(frame, pcm...)
	o.engine.SendBinary(c.ID(), frame)
}

// StreamAIAudio — TTS текста ИИ (greeting/подсказка) → бинарные кадры (через
// engine.SendBinary). Стриминг по предложениям (SplitSentences), паузы между
// ними (TTSGap), end-флаг на последнем кадре.
func (o *Orchestrator) StreamAIAudio(c Conn, voice Voice, text string) {
	if voice == nil || strings.TrimSpace(text) == "" {
		return
	}
	stop := c.BeginTTS() // barge-in: стрим можно прервать кандидатом
	defer c.EndTTS(stop)

	audioOK := func() bool { return c.Ctx().Err() == nil && !stopped(stop) && !c.SessStopped() }

	sentences := SplitSentences(text)

	seq := 0
	sentAny := false
	silence := make([]byte, ChunkBytes) // 250 мс тишины
	for i, sentence := range sentences {
		if !audioOK() {
			return
		}
		pcm, err := voice.TTS(c.Ctx(), PrepareTTS(sentence))
		if err != nil {
			o.log.Warn("tts: синтез предложения не удался", "session", c.ID(), "sentence", i+1, "err", err)
			continue
		}
		for off := 0; off < len(pcm); off += ChunkBytes {
			if !audioOK() {
				return
			}
			end := off + ChunkBytes
			if end > len(pcm) {
				end = len(pcm)
			}
			o.SendTTSFrame(c, uint16(seq), pcm[off:end], false)
			seq++
			sentAny = true
		}
		if i < len(sentences)-1 {
			gap := TTSGap(sentence)
			for g := 0; g < gap; g++ {
				if !audioOK() {
					return
				}
				o.SendTTSFrame(c, uint16(seq), silence, false)
				seq++
			}
		}
	}
	if sentAny && c.Ctx().Err() == nil && !stopped(stop) && !c.SessStopped() {
		o.SendTTSFrame(c, uint16(seq), silence, true)
	}
}
