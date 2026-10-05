package httpapi

import (
	"encoding/binary"
	"errors"
	"strings"
	"sync"
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
// Режим: turn-taking + barge-in (SRS §8): пока ход занят (LLM-фаза) — микрофон
// не слушается; пока ИИ говорит (TTS-стрим) — микрофон слушается, и
// завершённая VAD-реплика длиной ≥ BargeInMinSpeechMS прерывает речь ИИ
// (stop-канал + WS tts_stop), после чего обрабатывается как обычный ход.
// Контракт кадров S→C: 4-байтный заголовок {seq u16 LE, flags u16 LE} + PCM16
// 16 кГц mono; flags 0x01 — последний кадр потока (новые реплики — seq заново с 0).

const (
	ttsChunkBytes = 8000 // 250 мс @ 16 кГц PCM16 (ADR-001: кадры ~250 мс)
	ttsFlagEnd    = 0x01
	// frameDur — pacing: один кадр = 250 мс аудио, кадры уходят не чаще
	// одного за 250 мс (реальное время).
	frameDur = 250 * time.Millisecond
	// BargeInMinSpeechMS — минимальная длительность завершённой реплики, чтобы
	// прервать речь ИИ (barge-in): защита от ложных срабатываний (эхо,
	// дыхание, покашливание). Короткие всплески VAD всё равно отбрасывает.
	BargeInMinSpeechMS = 500
	// TTSParallelism — число TTS-воркеров (окно pipeline-синтеза): пока
	// предложение N отправляется в pacing, следующие TTSParallelism-1 уже
	// синтезируются (voice /tts stateless — параллельные запросы OK, ADR-008).
	TTSParallelism = 3
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
	var stop <-chan struct{}   // stop-канал TTS-стрима (barge-in)
	if voiceOn {
		stop = ws.beginTTS() // gate микрофона: сброс строго после end-кадра
		frames = make(chan []byte, 64)
		go func() {
			defer close(frames)
			s.produceOrderedTTS(ws, sentences, frames, stop)
		}()
		// Pacing параллельно LLM-стриму: кадры уходят, пока LLM ещё пишет.
		turnDone = make(chan struct{})
		go func() {
			defer close(turnDone)
			defer ws.endTTS(stop) // строго после end-кадра (или barge-in)
			_, _, ended := s.paceFrames(ws, start, frames, stop)
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

// ttsJob — предложение для TTS-синтеза. idx — порядковый номер (порядок
// вывода), gap — число тише-кадров перед предложением (по пунктуации
// предыдущего: 1 после «.»/«…», 2 после «?»/«!»).
type ttsJob struct {
	idx  int
	gap  int
	text string
}

// produceOrderedTTS — параллельный TTS-синтез (пул из TTSParallelism воркеров,
// очередь #3) с сохранением порядка вывода: пока предложение N отправляется в
// pacing, следующие TTSParallelism-1 уже синтезируются; однако кадры N (с
// предшествующей паузой) полностью уходят в frames до кадров N+1.
//
// Barge-in (stop закрыт): недиспетчеризованные предложения не начинают
// синтез, уже запущенные — доводят до конца, их результаты сливаются (без
// утечки горутины). Ошибка TTS на предложение — skip (warn-лог), остальные
// синтезируются.
func (s *Server) produceOrderedTTS(ws *wsSession, sentences <-chan string, frames chan<- []byte, stop <-chan struct{}) {
	// 1) Диспетчер: читает предложения по порядку, назначает idx/gap,
	// диспетчеризует в пул. jobs без буфера — окно ограничено числом воркеров.
	jobs := make(chan ttsJob)
	go func() {
		defer close(jobs)
		prev := ""
		idx := 0
		for sentence := range sentences {
			if stopped(stop) {
				return // barge-in: не начинаем синтез недиспетчеризованных
			}
			gap := 0
			if prev != "" {
				gap = 1
				if strings.HasSuffix(prev, "?") || strings.HasSuffix(prev, "!") {
					gap = 2
				}
			}
			prev = sentence
			idx++
			jobs <- ttsJob{idx: idx, gap: gap, text: sentence}
		}
	}()

	// 2) Воркеры: параллельный синтез; результаты могут приходить не по порядку.
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
				pcm, err := s.voice.TTS(ws.ctx, prepareTTS(j.text))
				if err != nil {
					metrics.TTSErrors.Inc(nil)
					s.log.Warn("tts: не удалось синтезировать предложение", "session", ws.id, "err", err)
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

	// 3) Записчик: собирает результаты по idx (реордер) и отправляет в frames
	// строго по порядку. При barge-in перестаёт слать (end-кадр шлёт
	// paceFrames), но продолжает сливать результаты.
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
			if !pushTTSJob(ws, frames, stop, cur.gap, cur.pcm) {
				bargedIn = true
			}
		}
	}
}

// pushTTSJob — кадры предложения (тише-пауза + PCM) в frames. select на stop
// (barge-in) и ws.ctx (конец сессии): при любом из них перестаёт слать —
// false (end-кадр шлёт paceFrames).
func pushTTSJob(ws *wsSession, frames chan<- []byte, stop <-chan struct{}, gap int, pcm []byte) bool {
	silence := make([]byte, ttsChunkBytes)
	send := func(b []byte) bool {
		select {
		case frames <- b:
			return true
		case <-stop:
			return false
		case <-ws.ctx.Done():
			return false
		}
	}
	for g := 0; g < gap; g++ {
		if !send(silence) {
			return false
		}
	}
	for off := 0; off < len(pcm); off += ttsChunkBytes {
		end := off + ttsChunkBytes
		if end > len(pcm) {
			end = len(pcm)
		}
		if !send(pcm[off:end]) {
			return false
		}
	}
	return true
}

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
// только на финальном кадре (тишина 250 мс после последнего аудио). Возвращает
// время первого/последнего кадра и завершён ли поток end-кадром (ctx-отмена — нет).
// stop — barge-in: закрыт канал — поток завершается end-кадром немедленно
// (новые кадры не идут).
func (s *Server) paceFrames(ws *wsSession, turnStart time.Time, frames chan []byte, stop <-chan struct{}) (time.Time, time.Time, bool) {
	var seq uint16
	silence := make([]byte, ttsChunkBytes)
	ticker := time.NewTicker(25 * time.Millisecond) // разрешение pacing-списка
	defer ticker.Stop()
	var queue [][]byte     // накопленные кадры (LLM/TTS обгоняют pacing)
	var nextSend time.Time // zero = можно отправлять сразу (первый кадр)
	var firstAt, endAt time.Time
	sentAny := false
	closed := false // TTS-воркер завершил поставку кадров
	for {
		if ws.ctx.Err() != nil {
			return firstAt, endAt, false
		}
		if stopped(stop) {
			// barge-in: прерываемый стрим завершаем end-кадром; PCM больше не шлём.
			if sentAny {
				s.sendTTSFrame(ws, seq, silence, true)
				endAt = time.Now()
			}
			return firstAt, endAt, sentAny
		}
		now := time.Now()
		// Финальный end-кадр: все кадры получены и отправлены (по pacing-ритму).
		if closed && len(queue) == 0 && !nextSend.After(now) {
			if !sentAny {
				return firstAt, endAt, false // кадров не было (все TTS-ошибки) — stop не нужен
			}
			s.sendTTSFrame(ws, seq, silence, true)
			return firstAt, now, true
		}
		if len(queue) > 0 && !nextSend.After(now) {
			hold := queue[0]
			queue = queue[1:]
			frame := make([]byte, 0, 4+len(hold))
			var hdr [4]byte
			binary.LittleEndian.PutUint16(hdr[0:2], seq)
			frame = append(frame, hdr[:]...)
			frame = append(frame, hold...)
			s.engine.SendBinary(ws.id, frame)
			seq++
			if !sentAny {
				firstAt = now
				metrics.TurnStage.Observe(metrics.StageLabel("tts_first_frame"), now.Sub(turnStart).Seconds())
			}
			sentAny = true
			nextSend = now.Add(frameDur)
			continue
		}
		if !closed {
			select {
			case <-ticker.C:
			case pcm, ok := <-frames:
				if !ok {
					closed = true // дальше — только pacing очереди (без чтения закрытого канала)
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

// sendTTSFrame — бинарный кадр TTS {seq u16 LE, flags u16 LE}+PCM16 (ADR-001).
func (s *Server) sendTTSFrame(ws *wsSession, seq uint16, pcm []byte, end bool) {
	frame := make([]byte, 0, 4+len(pcm))
	var hdr [4]byte
	binary.LittleEndian.PutUint16(hdr[0:2], seq)
	if end {
		binary.LittleEndian.PutUint16(hdr[2:4], ttsFlagEnd)
	}
	frame = append(frame, hdr[:]...)
	frame = append(frame, pcm...)
	s.engine.SendBinary(ws.id, frame)
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
	stop := ws.beginTTS() // barge-in: стрим можно прервать кандидатом
	defer ws.endTTS(stop)

	seq := 0
	sentAny := false                       // уходил ли хоть один кадр (для финального end-кадра)
	silence := make([]byte, ttsChunkBytes) // 250 мс тишины
	for i, sentence := range sentences {
		if ws.ctx.Err() != nil || stopped(stop) {
			return // barge-in: кадры больше не шлём (tts_stop уже ушёл)
		}
		pcm, err := s.voice.TTS(ws.ctx, prepareTTS(sentence))
		if err != nil {
			s.log.Warn("tts: синтез предложения не удался", "session", ws.id, "sentence", i+1, "err", err)
			// Degrade: пропускаем предложение, идём дальше (голос не критичен, текст уже есть).
			continue
		}
		for off := 0; off < len(pcm); off += ttsChunkBytes {
			if ws.ctx.Err() != nil || stopped(stop) {
				return
			}
			end := off + ttsChunkBytes
			if end > len(pcm) {
				end = len(pcm)
			}
			s.sendTTSFrame(ws, uint16(seq), pcm[off:end], false)
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
				if ws.ctx.Err() != nil || stopped(stop) {
					return
				}
				s.sendTTSFrame(ws, uint16(seq), silence, false)
				seq++
			}
		}
	}
	// Финальный кадр с end-флагом: чистый хвост (тишина 250 мс) и гарантия
	// остановки плеера. Только если реплики реально озвучивались и стрим не
	// был прерван barge-in (клиент уже получил tts_stop).
	if sentAny && ws.ctx.Err() == nil && !stopped(stop) {
		s.sendTTSFrame(ws, uint16(seq), silence, true)
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

// feedVAD — бинарный кадр PCM из readLoop.
// Стриминговый STT (ADR-007, дефолт): кадры уходят в voice /stt/stream —
// VAD (Silero) и распознавание на стороне voice: state/partial/final-
// события обрабатываются onSTTStreamEvent (partial → WS stt_partial, final →
// ход кандидата; barge-in по final, если реплика ≥ BargeInMinSpeechMS).
// Deградация: voice /stt/stream недоступен (sttStreamDown) или voice не
// подключён (s.voice == nil) — batch-путь (энергетический VAD + pre-STT +
// /stt, feedVADBatch); при переходе — Warn-лог + метрика (однократно).
// Turn-taking общий для обоих путей: пока ход занят И ИИ не говорит
// (LLM-фаза) — кадры не слушаются.
func (s *Server) feedVAD(ws *wsSession, pcm []byte) {
	// Ход занят, но ИИ молчит (LLM-фаза) — не слушаем (turn-taking, ADR-002).
	if ws.busy.Load() && !ws.ttsActive.Load() {
		return
	}
	if !ws.sttStreamDown.Load() {
		if st := ws.sttStreamFor(s); st != nil {
			st.Send(pcm)
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
			if w.ttsActive.Load() {
				time.AfterFunc(BargeInMinSpeechMS*time.Millisecond,
					func() { w.streamBargeIn(s) })
			}
		} else {
			w.sttSpeechSince.Store(0)
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
	s.log.Info("barge-in: кандидат прервал речь ИИ (стрим, во время речи)", "session", w.id)
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
	if w.ttsActive.Load() {
		if !w.sttBarged.Load() && ms < BargeInMinSpeechMS {
			s.log.Debug("barge-in: короткая реплика (стрим), без прерывания", "session", w.id, "ms", ms)
			return // защита от ложных срабатываний (эхо, дыхание)
		}
		if w.sttBarged.CompareAndSwap(false, true) {
			w.stopTTS()
			metrics.BargeInsTotal.Inc(nil)
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
			w.runStreamTurn(s, text, ev.Confidence)
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
			w.runStreamTurn(s, text, ev.Confidence)
		}()
		return
	}
	if !w.busy.CompareAndSwap(false, true) {
		return // на границе занят — реплика теряется (допустимо в ходовом режиме)
	}
	go w.runStreamTurn(s, text, ev.Confidence)
}

// runStreamTurn — ход кандидата из стримингового STT (busy уже занят;
// сбрасывается defer'ом): валидация стадии/доверия → runCandidateTurn.
func (w *wsSession) runStreamTurn(s *Server, text string, conf float64) {
	defer w.busy.Store(false)
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
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", w.id, "conf", conf, "text", text[:32])
		return
	}
	s.log.Info("stt: реплика кандидата", "session", w.id, "chars", len(text), "conf", conf, "stream", true)
	s.runCandidateTurn(w, text)
}

// feedVADBatch — batch-путь (fallback ADR-007 / voice без стрима): локальный
// энергетический VAD; по завершённой реплике — конвейер (pre-STT + /stt).
// Barge-in: пока ИИ говорит (ttsActive) — завершённая реплика длиной ≥
// BargeInMinSpeechMS прерывает TTS (stop-канал, WS tts_stop, метрика
// BargeInsTotal, log Info) и обрабатывается как обычный ход.
// Pre-STT: при первой тишине после речи (vad.PreSilence) запускаем
// распознавание на текущем буфере — оно перекрывает остаток VAD-хвоста.
// Инвалидация при возобновлении речи (новые speech-кадры расширяют буфер →
// preSTTBytes не совпадёт).
func (s *Server) feedVADBatch(ws *wsSession, pcm []byte) {
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
	if ws.ttsActive.Load() {
		// Кандидат заговорил, пока ИИ говорит.
		ms := int(int64(len(utterance)) * 1000 / 2 / 16000)
		if ms < BargeInMinSpeechMS {
			s.log.Debug("barge-in: короткая реплика, без прерывания", "session", ws.id, "ms", ms)
			return // защита от ложных срабатываний (эхо, дыхание)
		}
		ws.stopTTS()
		metrics.BargeInsTotal.Inc(nil)
		s.log.Info("barge-in: кандидат прервал речь ИИ", "session", ws.id, "ms", ms)
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
