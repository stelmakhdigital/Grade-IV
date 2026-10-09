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
	// MIN_CLAUSE_CHARS — минимальная длина части (байты) для клиуз-дробления
	// (клиуза-уровневая диспетчизация TTS, T-20261008185701): части короче
	// порога не дробятся по «,»/«;»/«:»/«—» — короткие предложения уходят
	// целиком, длинные — клиузами (ранний запуск TTS до конца предложения).
	MIN_CLAUSE_CHARS = 48
)

// runCandidateTurn — ход кандидата: событие + transcript, ответ ИИ (SSE-стрим),
// ai_text (единым сообщением по завершении LLM-текста), TTS-кадры с pacing.
// Вызывается из readLoop (текстовый utterance) и из goroutine голосового STT.
func (s *Server) runCandidateTurn(ws *wsSession, text string) {
	ctx := ws.ctx
	// Статус — из движка (in-memory: ставится атомарно с паузой, DB-статус может
	// отставать на момент записи): paused/завершённая сессия не начинает ход (FR-S7).
	if snap, err := s.engine.Snapshot(ws.id); err != nil || snap.Status != models.StatusActive {
		return // сессия не активна (пауза/завершение) — ход не обрабатывается
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
			if stopped(stop) || ws.sessStopped.Load() {
				return // barge-in / pause-finish: не начинаем синтез недиспетчеризованных
			}
			gap := ttsGap(prev) // «?/!» → 2, «.»/«...» → 1, «,»/«;»/«:»/«—» → 0
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
// (barge-in), sessStopped (pause/finish, FR-S7) и ws.ctx (конец сессии): при
// любом из них перестаёт слать — false (end-кадр шлёт paceFrames).
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
		if ws.sessStopped.Load() {
			return false
		}
		if !send(silence) {
			return false
		}
	}
	for off := 0; off < len(pcm); off += ttsChunkBytes {
		if ws.sessStopped.Load() {
			return false
		}
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

// splitDeltas — LLM-стрим → готовые части для TTS + полный текст (ai_text).
// Клиуза-уровневая диспетчизация (T-20261008185701): часть считается готовой,
// когда по логике splitForTTS после неё начинается новая (предложение или
// клиуза длинного предложения, ≥ MIN_CLAUSE_CHARS); остаток — по завершении
// стрима.
func splitDeltas(start time.Time, deltas <-chan string, sentences chan<- string, fullText chan<- string) {
	var full, pending string
	for d := range deltas {
		if full == "" {
			metrics.TurnStage.Observe(metrics.StageLabel("llm_first_token"), time.Since(start).Seconds())
		}
		full += d
		pending += d
		parts, tail := splitForTTSStreaming(pending)
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
		if stopped(stop) || ws.sessStopped.Load() {
			// barge-in / pause-finish (FR-S7): прерываемый стрим завершаем
			// end-кадром (если кадры уже уходили); PCM больше не шлём (tts_stop
			// клиенту отправлен в stopTTSOnAction).
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
	stop := ws.beginTTS() // barge-in: стрим можно прервать кандидатом
	defer ws.endTTS(stop)

	// Гейт по кадру: barge-in / pause-finish (FR-S7: sessStopped) / конец сессии.
	audioOK := func() bool { return ws.ctx.Err() == nil && !stopped(stop) && !ws.sessStopped.Load() }

	sentences := splitSentences(text)

	seq := 0
	sentAny := false                       // уходил ли хоть один кадр (для финального end-кадра)
	silence := make([]byte, ttsChunkBytes) // 250 мс тишины
	for i, sentence := range sentences {
		if !audioOK() {
			return // barge-in / pause-finish: кадры больше не шлём (tts_stop уже ушёл)
		}
		pcm, err := s.voice.TTS(ws.ctx, prepareTTS(sentence))
		if err != nil {
			s.log.Warn("tts: синтез предложения не удался", "session", ws.id, "sentence", i+1, "err", err)
			// Degrade: пропускаем предложение, идём дальше (голос не критичен, текст уже есть).
			continue
		}
		for off := 0; off < len(pcm); off += ttsChunkBytes {
			if !audioOK() {
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
		// Пауза между частями (не ставим после последнего): «?/!» → 2 (500 мс),
		// «.»/«...» → 1 (250 мс), «,»/«;»/«:»/«—» → 0 (клиузы одной фразы
		// играют подряд, T-20261008185701).
		if i < len(sentences)-1 {
			gap := ttsGap(sentence)
			for g := 0; g < gap; g++ {
				if !audioOK() {
					return
				}
				s.sendTTSFrame(ws, uint16(seq), silence, false)
				seq++
			}
		}
	}
	// Финальный кадр с end-флагом: чистый хвост (тишина 250 мс) и гарантия
	// остановки плеера. Только если реплики реально озвучивались и стрим не
	// был прерван barge-in/pause-finish (клиент уже получил tts_stop).
	if sentAny && ws.ctx.Err() == nil && !stopped(stop) && !ws.sessStopped.Load() {
		s.sendTTSFrame(ws, uint16(seq), silence, true)
	}
}

// splitSentences — разбивка текста на предложения для TTS-стриминга:
// «!»/«?» — всегда (дальше пробел/конец); «.» — только перед ЗАГЛАВНОЙ буквой
// (лат/кирл) или в конце текста — русские аббревиатуры («т.д.», «т.п.») с
// малой буквой не дробят; многоточие «...» — разрыв.
func splitSentences(text string) []string {
	parts, tail := splitSentencesTail(text)
	if p := strings.TrimSpace(tail); p != "" {
		parts = append(parts, p)
	}
	return parts
}

// splitSentencesTail — то же, что splitSentences, но возвращает также ОТКРЫТЫЙ
// хвост — текст после последней границы БЕЗ обрезки (raw). splitDeltas
// приписывает к нему токены: обрезка (TrimSpace) съела бы хвостовые/начальные
// пробелы и склеила слова («Отвечу» + «коротко » → «Отвечукоротко»), поэтому
// сканирование идёт по raw-тексту.
func splitSentencesTail(text string) ([]string, string) {
	if strings.TrimSpace(text) == "" {
		return nil, text // только пробелы — хвост как есть (токены припишутся)
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
	return parts, text[start:]
}

// splitForTTS — разбивка на части для TTS-диспетчизации: предложения
// (splitSentences) + дополнительно клиузы длинных частей (≥ MIN_CLAUSE_CHARS)
// по границам «,» «;» «:» «—» (клиуза-уровневая диспетчизация, T-20261008185701).
// Граница остаётся в конце клиузы; завершитель предложения — в последнем
// клиузе. Короткие предложения не дробятся. Семантика splitSentences
// сохранена (она используется отдельно — greeting/streamAIAudio, тесты).
func splitForTTS(text string) []string {
	parts, tail := splitForTTSStreaming(text)
	if p := strings.TrimSpace(tail); p != "" {
		parts = append(parts, p)
	}
	return parts
}

// splitForTTSStreaming — text → ЗАКРЫТЫЕ части для TTS + открытый хвост (raw,
// без обрезки — см. splitSentencesTail). Все предложения в sents закрыты
// (дробим в клиузы); открытый хвост (tailSent) — тоже дробим, но последняя
// открытая клиуза остаётся хвостом. splitDeltas: закрытые части → канал,
// хвост — остаётся pending (накопление до следующих границ).
func splitForTTSStreaming(text string) ([]string, string) {
	sents, tailSent := splitSentencesTail(text)
	var parts []string
	for _, s := range sents {
		cl, tail := splitClausesWithTail(s)
		// Завершитель предложения не является границей клиуз — последний
		// клиуз закрытого предложения добирается из хвоста.
		if p := strings.TrimSpace(tail); p != "" {
			cl = append(cl, p)
		}
		if len(cl) == 0 {
			cl = []string{s} // границ не было — предложение целиком
		}
		parts = append(parts, cl...)
	}
	cl, tail := splitClausesWithTail(tailSent)
	parts = append(parts, cl...)
	return parts, tail
}

// splitClauses — делит часть (предложение или открытую последнюю) на клиузы
// по «,» «;» «:» «—», если длина ≥ MIN_CLAUSE_CHARS. Граница — в конце клиузы;
// «:» внутри чисел («12:30») не дробит. Часть без границ — как есть (одна часть).
func splitClauses(part string) []string {
	out, tail := splitClausesWithTail(part)
	if p := strings.TrimSpace(tail); p != "" {
		out = append(out, p)
	}
	return out
}

// splitClausesWithTail — то же, что splitClauses, но возвращает закрытые
// клиузы (без последней открытой) + открытый хвост (raw, без обрезки).
func splitClausesWithTail(part string) ([]string, string) {
	if part == "" {
		return nil, ""
	}
	if len(part) < MIN_CLAUSE_CHARS {
		return nil, part
	}
	var out []string
	start := 0
	for i := 0; i < len(part); i++ {
		r, size := utf8.DecodeRuneInString(part[i:])
		isColon := false
		if r == ':' {
			// «12:30» не дробим: «:» между цифрами — часть числа.
			digitBefore := i > 0 && part[i-1] >= '0' && part[i-1] <= '9'
			digitAfter := i+size < len(part) && part[i+size] >= '0' && part[i+size] <= '9'
			isColon = !(digitBefore && digitAfter)
		}
		if r == ',' || r == ';' || r == '—' || isColon {
			c := strings.TrimSpace(part[start : i+size])
			switch {
			case c == "":
				start = i + size // фрагмент из одних пробелов — сбрасываем
			case !hasWordChar(c):
				// клиуза без букв (одна граница, «—»): в TTS не идёт,
				// граница остаётся в начале следующего фрагмента
				start = i
			default:
				out = append(out, c)
				start = i + size
			}
		}
	}
	return out, part[start:]
}

// hasWordChar — есть ли в s хотя бы одна буква/цифра (кирл/лат/цифры):
// фрагмент только из знаков препинания (например, одно «—») не отправляется
// в TTS отдельной частью.
func hasWordChar(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			(r >= 0x0400 && r <= 0x04FF) {
			return true
		}
	}
	return false
}

// ttsGap — число тише-кадров (250 мс) перед частью TTS по завершайющему знаку
// ПРЕДЫДУЩЕЙ части (клиуза-уровневая диспетчизация, T-20261008185701):
// «?/!» → 2 (500 мс), «.»/«...» → 1 (250 мс), «,»/«;»/«:»/«—» → 0
// (клиузы одной фразы играют подряд, без тише-кадров).
func ttsGap(prev string) int {
	if prev == "" {
		return 0
	}
	switch {
	case strings.HasSuffix(prev, "?"), strings.HasSuffix(prev, "!"):
		return 2
	case strings.HasSuffix(prev, "."):
		return 1
	case strings.HasSuffix(prev, ","), strings.HasSuffix(prev, ";"),
		strings.HasSuffix(prev, ":"), strings.HasSuffix(prev, "—"):
		return 0
	}
	return 1 // без завершителя (хвост оборванного стрима) — консервативно
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
		ms := totalMS - prerollMS
		if ms < 0 {
			ms = 0
		}
		if ms < BargeInMinSpeechMS {
			s.log.Debug("barge-in: короткая реплика, без прерывания", "session", ws.id, "ms", ms, "preroll_ms", prerollMS)
			return // защита от ложных срабатываний (эхо, дыхание, pre-roll)
		}
		ws.stopTTS()
		metrics.BargeInsTotal.Inc(nil)
		metrics.BargeInSpeechMS.Observe(nil, float64(ms))
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
	// Фильтр доверия: STT на шуме/дыхании галлюцинирует с низкой уверенностью.
	// Реальные реплики — conf ≥ 0.7 (замеры). Порог 0.5: ниже — шум, без хода.
	if res.Confidence < 0.5 {
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", ws.id, "conf", res.Confidence, "text", truncateForLog(text, 32))
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
	if ws.busy.Load() && !ws.ttsActive.Load() {
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
	// Длительность речи (wall, от state=true): стрим-путь — реальное время
	// (аудио в реальном времени ⇒ wall ≈ аудио-длительность). Без pre-roll.
	ms := int(time.Since(time.Unix(0, w.sttSpeechSince.Load())).Milliseconds())
	metrics.BargeInSpeechMS.Observe(nil, float64(ms))
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
	if w.ttsActive.Load() {
		if !w.sttBarged.Load() && ms < BargeInMinSpeechMS {
			s.log.Debug("barge-in: короткая реплика (стрим), без прерывания", "session", w.id, "ms", ms)
			return // защита от ложных срабатываний (эхо, дыхание)
		}
		if w.sttBarged.CompareAndSwap(false, true) {
			w.stopTTS()
			metrics.BargeInsTotal.Inc(nil)
			metrics.BargeInSpeechMS.Observe(nil, float64(ms)) // speech_ms из Silero VAD (без pre-roll)
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
		s.log.Debug("stt: низкое доверие — шум, без хода", "session", w.id, "conf", conf, "text", truncateForLog(text, 32))
		return
	}
	s.log.Info("stt: реплика кандидата", "session", w.id, "chars", len(text), "conf", conf, "stream", true)
	s.runCandidateTurn(w, text)
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
