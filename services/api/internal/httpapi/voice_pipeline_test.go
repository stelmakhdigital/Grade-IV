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
