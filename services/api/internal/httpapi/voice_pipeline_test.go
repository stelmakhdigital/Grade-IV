package httpapi

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/llm"
	"nhooyr.io/websocket"
)

// TestSplitSentences — разбивка текста на предложения (русские .!?… + многоточие).
func TestSplitSentences(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Привет! Как дела?", 2},
		{"Первое. Второе. Третье.", 3},
		{"Одно предложение", 1},
		{"Т.д. и т.п.", 1}, // короткие фрагменты сливаются
		{"", 0},
	}
	for _, c := range cases {
		got := splitSentences(c.in)
		if len(got) != c.want {
			t.Errorf("splitSentences(%q) = %v (len %d), want len %d", c.in, got, len(got), c.want)
		}
	}
}

// TestPrepareTTS — латинские термины транслитерируются для TTS,
// кириллица/цифры/пунктуация без изменений.
func TestPrepareTTS(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"какие проекты делал на Go", "какие проекты делал на гоу"},
		{"расскажи про Go и Python", "расскажи про гоу и питон"},
		{"использовали C++ и Rust", "использовали си плюс и раст"},
		{"сервис на Go, база Postgres", "сервис на гоу, база постгрес"},
		{"123 и 45%", "123 и 45%"},                 // цифры/проценты
		{"Привет! Как дела?", "Привет! Как дела?"}, // чистый русский
		{"Go-сервис", "гоу-сервис"},                // слово в составе через дефис
	}
	for _, c := range cases {
		got := prepareTTS(c.in)
		if got != c.want {
			t.Errorf("prepareTTS(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// wsTTSFrame — бинарный кадр TTS: заголовок {seq,flags}+PCM.
type wsTTSFrame struct {
	seq int
	end bool
}

// bargeInEnv — общий пролог barge-in-тестов: mock-voice с длинным ответом
// (12 pacing-кадров × 250 мс = 3 с), старт сессии, прочитанное приветствие
// (до end-кадра), текстовая реплика (ход ИИ с pacing) и первые 2 кадра
// ответа (ttsActive=true). Возвращает первый seq ответа (каждый поток реплики —
// seq заново с 0, ADR-001).
func bargeInEnv(t *testing.T) (ts *httptest.Server, conn *websocket.Conn, firstSeq int) {
	t.Helper()
	tsServer, token, sessionID, m, _ := newVoiceEnv(t)
	m.ttsPCMSize = 96000 // 3 с аудио = 12 pacing-кадров хода ИИ
	conn = dialWS(t, tsServer, token, sessionID)

	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	// TTS приветствия: читаем до end-кадра (число кадров зависит от числа
	// предложений мок-приветствия: аудио + паузы + end-тишина).
	deadline := time.Now().Add(10 * time.Second)
	greetingEnd := false
	for !greetingEnd && time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" && fr.end {
			greetingEnd = true
		}
	}
	if !greetingEnd {
		t.Fatal("TTS-приветствие не завершено end-кадром")
	}
	// Текстовая реплика → ход ИИ (pacing). Первые 2 кадра — поток идёт.
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "Привет"}}))
	cancel()
	bins := 0
	deadline = time.Now().Add(5 * time.Second)
	for bins < 2 && time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" {
			if bins == 0 {
				firstSeq = fr.seq
			}
			bins++
		}
	}
	if bins < 2 {
		t.Fatal("TTS-кадры ответа ИИ не пришли (pacing не стартовал)")
	}
	return tsServer, conn, firstSeq
}

// bargeInReadOne — одно WS-сообщение: текст (type/who) или бинарный кадр TTS.
func bargeInReadOne(t *testing.T, conn *websocket.Conn) (typ, who string, fr wsTTSFrame, ok bool) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 500*time.Millisecond)
	mt, data, err := conn.Read(ctx)
	cancel()
	if err != nil {
		return "", "", wsTTSFrame{}, false
	}
	if mt == websocket.MessageBinary {
		if len(data) >= 4 {
			fr.seq = int(binary.LittleEndian.Uint16(data[0:2]))
			fr.end = binary.LittleEndian.Uint16(data[2:4])&0x01 != 0
		}
		return "bin", "", fr, true
	}
	var mm map[string]any
	_ = json.Unmarshal(data, &mm)
	return fmt.Sprint(mm["type"]), fmt.Sprint(mm["who"]), wsTTSFrame{}, true
}

// TestBargeIn_InterruptsTTS — кандидат говорит (реплика ≥ 500 мс) во время
// TTS-стрима хода ИИ: (1) текущий TTS-конвейер немедленно останавливается —
// новых PCM-кадров прерываемой реплики нет, поток завершён end-кадром;
// (2) реплика кандидата дала transcript/новый ход (новый TTS-стрим, seq с 0);
// (3) метрика grade_barge_ins_total = 1.
func TestBargeIn_InterruptsTTS(t *testing.T) {
	ts, conn, firstSeq := bargeInEnv(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Реплика кандидата во время речи ИИ: тон 2×250 мс (500 мс ≥ порога)
	// + тишина 3×250 мс (хвост VAD 500 мс → реплика завершена).
	for i := 0; i < 2; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	// (1) tts_stop + end-кадр прерванного стрима; старая реплика не доиграна.
	sawTTSStop, sawEnd := false, false
	oldFrames, oldEndSeq := 0, -1
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !(sawTTSStop && sawEnd) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		switch typ {
		case "tts_stop":
			sawTTSStop = true
		case "bin":
			if sawTTSStop {
				oldFrames++
				if fr.end {
					sawEnd, oldEndSeq = true, fr.seq
				}
			}
		}
	}
	if !sawTTSStop {
		t.Fatal("нет WS-сообщения tts_stop")
	}
	if !sawEnd {
		t.Fatal("прерванный TTS-поток не завершён end-кадром")
	}
	if oldFrames != 1 {
		t.Fatalf("после tts_stop ушло кадров прерываемой реплики: %d (want 1 — только end-кадр)", oldFrames)
	}
	// Полный не прерванный поток = 13 кадров (12 аудио + end-тишина): end на
	// firstSeq+12. Прерванный — раньше (в прологе прочитаны firstSeq, firstSeq+1).
	if oldEndSeq > firstSeq+11 || oldEndSeq < firstSeq+2 {
		t.Fatalf("end-кадр на seq=%d (firstSeq=%d) — поток не прерван (или прерван до начала)", oldEndSeq, firstSeq)
	}

	// (2) реплика кандидата → transcript(user) → новый ход (ai_text + TTS, seq с 0).
	var sawUser, sawAIText bool
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !(sawUser && sawAIText) {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "transcript" && who == "user" {
			sawUser = true
		}
		if typ == "ai_text" {
			sawAIText = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от прерывающей реплики")
	}
	if !sawAIText {
		t.Fatal("нет ai_text нового хода")
	}
	// Первый кадр нового TTS-стрима — seq заново с 0 (новый поток реплики, ADR-001).
	deadline = time.Now().Add(5 * time.Second)
	var newSeq int
	for time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" {
			newSeq = fr.seq
			break
		}
	}
	if newSeq != 0 {
		t.Fatalf("первый кадр нового TTS-стрима seq=%d (want 0 — seq заново с 0)", newSeq)
	}

	// (3) метрика barge-in.
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if got := string(body); !containsLine(got, "grade_barge_ins_total 1") {
		t.Fatalf("метрика barge-in: ожидается строка 'grade_barge_ins_total 1' в /metrics")
	}
}

// TestBargeIn_ShortUtteranceDoesNotInterrupt — короткий burst (< 500 мс) во
// время TTS прерывание НЕ вызывает: стрим доигрывается до естественного конца
// (end-кадр на последнем seq), tts_stop нет, нового хода нет.
func TestBargeIn_ShortUtteranceDoesNotInterrupt(t *testing.T) {
	_, conn, firstSeq := bargeInEnv(t)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Короткий burst: тон 2×200 мс (400 мс < BargeInMinSpeechMS) + тишина 2×250 мс.
	for i := 0; i < 2; i++ {
		writePCM(tonePCM(200, 5000))
	}
	for i := 0; i < 2; i++ {
		writePCM(silencePCM(250))
	}

	// Стрим ИИ доигрывается до конца: end-флаг на последнем кадре потока
	// (firstSeq+12: 12 аудио + end-тишина), tts_stop не наблюдался.
	sawTTSStop := false
	endSeq := -1
	seqs := []int{}
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && endSeq < 0 {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "tts_stop" {
			sawTTSStop = true
		}
		if typ == "bin" && fr.seq >= firstSeq {
			seqs = append(seqs, fr.seq)
			if fr.end {
				endSeq = fr.seq
			}
		}
	}
	if sawTTSStop {
		t.Fatal("короткий burst (< 500 мс) вызвал tts_stop — barge-in сработал ложно")
	}
	if endSeq != firstSeq+12 {
		t.Fatalf("end-кадр на seq=%d (want %d — стрим доигран полностью), seqs=%v", endSeq, firstSeq+12, seqs)
	}
	// Новый ход не был запущен: нет transcript(user) в ближайших сообщениях.
	deadline = time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			return
		}
		if typ == "transcript" && who == "user" {
			t.Fatal("короткий burst дал ход кандидата (transcript user) — не должно быть")
		}
	}
}

// containsLine — есть ли строка с префиксом в тексте (по строкам).
func containsLine(s, prefix string) bool {
	for _, line := range splitLines(s) {
		if len(line) >= len(prefix) && line[:len(prefix)] == prefix {
			return true
		}
	}
	return false
}

func splitLines(s string) []string {
	var out []string
	cur := 0
	for i := 0; i < len(s); i++ {
		if s[i] == '\n' {
			out = append(out, s[cur:i])
			cur = i + 1
		}
	}
	if cur < len(s) {
		out = append(out, s[cur:])
	}
	return out
}

// TestTranslitWord — фолбэк по буквам: детерминированность, капс первого
// символа, цифры/кириллица на месте.
func TestTranslitWord(t *testing.T) {
	if got := translitWord("Hello"); got != "Хэлло" {
		t.Errorf("translitWord(Hello) = %q, want %q", got, "Хэлло")
	}
	if got := translitWord("hello"); got != "хэлло" {
		t.Errorf("translitWord(hello) = %q, want %q", got, "хэлло")
	}
	if got := translitWord("z"); got != "з" {
		t.Errorf("translitWord(z) = %q, want %q", got, "з")
	}
}

// TestTTSStreaming — стриминг по предложениям: mock voice считает вызовы TTS.
func TestTTSStreaming(t *testing.T) {
	// Тексты: 2 предложения → 2 вызова TTS.
	text := "Привет! Как дела?"
	sentences := splitSentences(text)
	if len(sentences) != 2 {
		t.Fatalf("sentences = %v, want 2", sentences)
	}
	// Проверка: первое предложение короче полного текста (стриминг).
	if len(sentences[0]) >= len(text) {
		t.Fatalf("первое предложение не короче полного текста: %v", sentences)
	}
}

// pcmPeak — пиковая амплитуда PCM16LE (маркер предложения в мок-TTS).
func pcmPeak(pcm []byte) int {
	peak := 0
	for i := 0; i+1 < len(pcm); i += 2 {
		v := int(int16(binary.LittleEndian.Uint16(pcm[i:])))
		if v < 0 {
			v = -v
		}
		if v > peak {
			peak = v
		}
	}
	return peak
}

// TestTTSParallelSynthesis — pipeline TTS-синтез (очередь #3): при искусственной
// задержке синтеза ≥ pacing-окна предложения (2 кадра = 500 мс; delay 700 мс)
// второе предложение начинает синтезироваться ДО конца синтеза первого
// (перекрытие — по времени вызовов mock /tts), а порядок вывода сохранён:
// кадры первого предложения (амплитудный маркер A1) полностью предшествуют
// кадрам второго (A2).
func TestTTSParallelSynthesis(t *testing.T) {
	ts, token, sessionID, m, e := newVoiceEnv(t)
	// Мок-LLM: ответ из 2 предложений (приветствие и ход — один и тот же).
	e.mock.SetResponder(func(req llm.Request) (string, error) {
		return "Привет! Как дела?", nil
	})
	// Мок-TTS: задержка синтеза ≥ pacing-окна предложения + амплитуда на
	// предложение (маркер предложения в PCM для проверки порядка).
	m.ttsPCMSize = 16000 // 500 мс = 2 pacing-кадра на предложение
	m.ttsDelay = 700 * time.Millisecond
	m.ttsAmp = map[string]int{"Привет!": 1000, "Как дела?": 2000}

	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Приветствие: пролистать до TTS end-кадра.
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	// Читаем до TTS end-кадра. Таймаут Read (2 с) длиннее паузы синтеза
	// (700 мс): nhooyr закрывает соединение при истечении ctx у Read, поэтому
	// короткий таймаут (как 500 мс в bargeInReadOne) оборвал бы conn во время TTS-паузы.
	greetEnd := false
	gd := time.Now().Add(10 * time.Second)
	for !greetEnd && time.Now().Before(gd) {
		rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
		mt, data, rerr := conn.Read(rctx)
		rcancel()
		if rerr != nil || mt != websocket.MessageBinary || len(data) < 4 {
			continue
		}
		if binary.LittleEndian.Uint16(data[2:4])&0x01 != 0 {
			greetEnd = true
		}
	}
	if !greetEnd {
		t.Fatal("TTS-приветствие не завершено end-кадром")
	}
	greetBase := len(m.TTSStarts())

	// Ход кандидата (текстовая реплика) → ответ ИИ «Привет! Как дела?».
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "расскажи"}}))
	cancel()

	// Ход ИИ: читать до TTS end-кадра, собирать амплитудные маркеры кадров.
	var markers []int
	sawEnd := false
	dl := time.Now().Add(15 * time.Second)
	for !sawEnd && time.Now().Before(dl) {
		rctx, rcancel := context.WithTimeout(context.Background(), 2*time.Second)
		mt, data, rerr := conn.Read(rctx)
		rcancel()
		if rerr != nil || mt != websocket.MessageBinary || len(data) < 8 {
			continue
		}
		if binary.LittleEndian.Uint16(data[2:4])&0x01 != 0 {
			sawEnd = true
		}
		markers = append(markers, pcmPeak(data[4:]))
	}
	if !sawEnd {
		t.Fatalf("TTS end-кадр хода ИИ не пришёл (markers=%v)", markers)
	}

	// (1) Порядок: кадры A1 (первое предложение) полностью предшествуют A2.
	firstA2, lastA1 := -1, -1
	for i, pk := range markers {
		switch {
		case pk >= 1500: // A2 (амплитуда 2000)
			if firstA2 < 0 {
				firstA2 = i
			}
		case pk >= 500: // A1 (амплитуда 1000), не тишина
			lastA1 = i
		}
	}
	if firstA2 < 0 {
		t.Fatalf("нет кадров второго предложения (markers=%v)", markers)
	}
	if lastA1 < 0 {
		t.Fatalf("нет кадров первого предложения (markers=%v)", markers)
	}
	if lastA1 >= firstA2 {
		t.Errorf("порядок нарушен: кадр первого предложения после второго (lastA1=%d firstA2=%d markers=%v)",
			lastA1, firstA2, markers)
	}

	// (2) Перекрытие: второе предложение начало синтез до конца синтеза первого.
	starts := m.TTSStarts()
	if len(starts) < greetBase+2 {
		t.Fatalf("ожидалось ≥ %d TTS-вызовов, получено %d", greetBase+2, len(starts))
	}
	s1, s2 := starts[greetBase], starts[greetBase+1]
	gap := s2.Sub(s1)
	if gap >= m.ttsDelay {
		t.Errorf("синтез не параллельный: второе предложение стартовало через %v после первого (ожидалось < delay %v) — последовательный синтез",
			gap, m.ttsDelay)
	}
	t.Logf("перекрытие синтеза: s2-s1=%v (delay=%v); маркеры=%v", gap, m.ttsDelay, markers)
}

// TestSTTStreamPartialBeforeTranscript — стриминговый STT (ADR-007):
// промежуточный текст (stt_partial из voice /stt/stream) приходит в UI
// ДО финального transcript(user) — partial перекрывает распознавание.
func TestSTTStreamPartialBeforeTranscript(t *testing.T) {
	ts, token, sessionID, _, _ := newVoiceEnv(t)
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Старт: stage + приветствие (ai_text).
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}

	// Говорим: тон 4×250 мс + тишина 3×250 мс (VAD voice завершает реплику).
	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	// Читаем до transcript(user): stt_partial с текстом должен быть раньше.
	sawPartial, sawUser := false, false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawUser {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "stt_partial" {
			sawPartial = true
		}
		if typ == "transcript" && who == "user" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от стримингового STT")
	}
	if !sawPartial {
		t.Fatal("нет stt_partial до transcript(user) — partial не пришёл")
	}
}

// TestSTTStreamFallbackToBatch — voice /stt/stream недоступен (HTTP 500):
// конвейер деградирует на batch-путь (энергетический VAD в Go + batch /stt):
// реплика кандидата даёт transcript(user), метрика-счётчик переключений = 1.
func TestSTTStreamFallbackToBatch(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.streamBroken = true // /stt/stream отвечает 500 → реконнекты не удаются
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Тон 2×250 мс — запускает стрим-клиент (реконнекты ~1.5 с).
	writePCM(tonePCM(250, 5000))
	writePCM(tonePCM(250, 5000))
	time.Sleep(2 * time.Second) // реконнекты 4×500 мс → "unavailable" → batch-путь
	// Реплика для batch-VAD (локальный): тон 4×250 мс + тишина 3×250 мс (хвост 500 мс).
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	sawUser := false
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawUser {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "transcript" && who == "user" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) — fallback на batch-путь не сработал")
	}
	if m.sttCalls < 1 {
		t.Fatal("batch /stt не вызывался после деградации")
	}
	resp, err := http.Get(ts.URL + "/metrics")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	body, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if !containsLine(string(body), "grade_stt_stream_fallbacks_total 1") {
		t.Fatal("нет метрики grade_stt_stream_fallbacks_total 1")
	}
}

// TestTruncateForLog: короткая строка не должна паниковать (баг 2026-10-08:
// text[:32] на 19-байтной реплике ронял процесс), длинные — ровно n байт
// без разрыва юникод-последовательности.
func TestTruncateForLog(t *testing.T) {
	if got := truncateForLog("абв", 32); got != "абв" {
		t.Fatalf("короткая: %q", got)
	}
	long := "abcdefghijklmnopqrstuvwxyz0123456789" // 36 байт
	if got := truncateForLog(long, 32); len(got) != 32 || got != long[:32] {
		t.Fatalf("длинная: %q (%d)", got, len(got))
	}
	// 32 байта — ровно на границе 2-байтовых букв (16 × 2): граница допустима.
	cyr := "абвгдежзиклмнопрст" // 18 символов × 2 = 36 байт
	got := truncateForLog(cyr, 32)
	if len(got) != 32 || got != cyr[:32] {
		t.Fatalf("юникод: %q (%d)", got, len(got))
	}
	// А 31 байт — внутри 16-й буквы: усечь до 30 (15 букв).
	if got := truncateForLog(cyr, 31); len(got) != 30 || got != cyr[:30] {
		t.Fatalf("юникод-2: %q (%d)", got, len(got))
	}
}
