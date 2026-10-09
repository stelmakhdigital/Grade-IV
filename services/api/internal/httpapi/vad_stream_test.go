package httpapi

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"time"

	"nhooyr.io/websocket"
)

// TestVADStreamPreSTT — Silero-путь (level 2, ADR-002 поправка 2026-10-09):
// voice /stt/stream недоступен → кадры в voice /vad/stream (mock). На
// pre_silence запускается pre-STT, на utterance — ход кандидата; pre-STT
// покрывает ходовой конвейер — sttCalls == 1 (повторного /stt нет).
func TestVADStreamPreSTT(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.streamBroken = true // level 1 мёртв → кадры идут в /vad/stream
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Старт: stage + приветствие (ai_text).
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" {
		t.Fatalf("старт: %v", msgs)
	}
	if !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("приветствие: %v", msgs[1])
	}
	// TTS приветствия: до конца (ttsPCMSize 48000 → 6 кадров).
	for i := 0; i < (m.ttsPCMSize+7999)/8000; i++ {
		got := wsReadMixed(t, conn, 1, 3*time.Second)
		if len(got) != 1 || !strings.HasPrefix(got[0], "bin:") {
			t.Fatalf("TTS-кадр приветствия %d: %v", i, got)
		}
	}

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Запускаем стt-стрим-клиент и даём ему деградировать (реконнекты ~1.5 с).
	writePCM(tonePCM(250, 5000))
	writePCM(tonePCM(250, 5000))
	time.Sleep(2000 * time.Millisecond)
	// Реплика через VAD-стрим: 4 тона (1 с) + тишина 3×250 мс
	// (mock: pre_silence на 2-м тише-кадре, end на 3-м).
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	var sawUser bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawUser {
		got := wsReadMixed(t, conn, 1, 2*time.Second)
		if len(got) == 0 {
			continue
		}
		if got[0] == "text:transcript/user/Здравствуйте, расскажите о себе" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от Silero-пути (/vad/stream + batch /stt)")
	}
	// Pre-STT (на pre_silence) покрыл ходовой конвейер: /stt вызван один раз.
	time.Sleep(200 * time.Millisecond)
	if m.sttCalls != 1 {
		t.Fatalf("sttCalls = %d, want 1 (pre-STT должен покрыть ходовой конвейер)", m.sttCalls)
	}
}

// TestVADStreamBargeIn — barge-in по Silero-пути: пока ИИ говорит (ttsActive)
// завершённая VAD-реплика (≥ BargeInMinSpeechMS) прерывает TTS: tts_stop,
// метрика BargeInsTotal = 1, реплика даёт ход кандидата.
func TestVADStreamBargeIn(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.streamBroken = true // level 1 мёртв → /vad/stream
	m.ttsPCMSize = 96000  // 3 с TTS на реплику ИИ (12 pacing-кадров)
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Старт: stage + приветствие.
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	// TTS приветствия: до end-кадра (12 кадров).
	greetingEnd := false
	deadline := time.Now().Add(10 * time.Second)
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
	// Текстовая реплика → ход ИИ (pacing, ttsActive=true).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "Привет"}}))
	cancel()
	bins := 0
	deadline = time.Now().Add(5 * time.Second)
	for bins < 2 && time.Now().Before(deadline) {
		typ, _, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" {
			bins++
		}
	}
	if bins < 2 {
		t.Fatal("TTS-кадры ответа ИИ не пришли (pacing не стартовал)")
	}

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Кадры речи: стt-стрим мёртв — первые кадры запускают реконнекты;
	// держим PCM-поток ~2 с (пока стt-стрим не деградировал и кадры не
	// ушли в vad-стрим — иначе реплика потеряется), затем тишина-хвост.
	t0 := time.Now()
	for time.Since(t0) < 2200*time.Millisecond {
		writePCM(tonePCM(250, 5000))
		time.Sleep(200 * time.Millisecond)
	}
	// Реплика завершена: тишина 3×250 мс (mock: pre_silence + end).
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	// tts_stop + transcript(user) от прерывающей реплики.
	sawTTSStop, sawUser := false, false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !(sawTTSStop && sawUser) {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "tts_stop" {
			sawTTSStop = true
		}
		if typ == "transcript" && who == "user" {
			sawUser = true
		}
	}
	if !sawTTSStop {
		t.Fatal("нет tts_stop — barge-in по Silero-пути не сработал")
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от прерывающей реплики")
	}
	body := metricsBody(t, ts.URL)
	if !metricAtLeast(body, "grade_barge_ins_total", 1) {
		t.Fatalf("нет метрики grade_barge_ins_total ≥ 1: %s", body)
	}
}

// TestVADStreamFallbackToEnergy — оба стрим-пути мёртвы (/stt/stream и
// /vad/stream → 500): деградация на energy-путь (level 3, last-resort) —
// реплика всё равно распознаётся; обе метрики деградации = 1.
func TestVADStreamFallbackToEnergy(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.streamBroken = true
	m.vadStreamBroken = true
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" {
		t.Fatalf("старт: %v", msgs)
	}
	// TTS приветствия: до конца.
	for i := 0; i < (m.ttsPCMSize+7999)/8000; i++ {
		got := wsReadMixed(t, conn, 1, 3*time.Second)
		if len(got) != 1 || !strings.HasPrefix(got[0], "bin:") {
			t.Fatalf("TTS-кадр приветствия %d: %v", i, got)
		}
	}

	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Держим PCM-поток ~3.4 с: стt-стрим деградирует (~1.5 с), кадры
	// переходят в vad-стрим, тот деградирует ещё ~1.5 с → energy-путь.
	t0 := time.Now()
	for time.Since(t0) < 3400*time.Millisecond {
		writePCM(tonePCM(250, 5000))
		time.Sleep(300 * time.Millisecond)
	}
	// Ждём обе деградации (метрики) — до отправки реплики (робастность тайминга).
	for i := 0; i < 50; i++ {
		body := metricsBody(t, ts.URL)
		if metricAtLeast(body, "grade_stt_stream_fallbacks_total", 1) &&
			metricAtLeast(body, "grade_vad_stream_fallbacks_total", 1) {
			break
		}
		writePCM(tonePCM(250, 5000)) // держим поток (vad-клиент ленивый)
		time.Sleep(200 * time.Millisecond)
	}
	// Реплика для energy VAD: 4 тона + тишина (хвост 500 мс → завершение).
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
	}

	var sawUser bool
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) && !sawUser {
		got := wsReadMixed(t, conn, 1, 2*time.Second)
		if len(got) == 0 {
			continue
		}
		if got[0] == "text:transcript/user/Здравствуйте, расскажите о себе" {
			sawUser = true
		}
	}
	if !sawUser {
		t.Fatal("нет transcript(user) — деградация на energy-путь не сработала")
	}
	if m.sttCalls < 1 {
		t.Fatal("batch /stt не вызывался после деградации")
	}
	body := metricsBody(t, ts.URL)
	if !metricAtLeast(body, "grade_stt_stream_fallbacks_total", 1) {
		t.Fatalf("нет метрики grade_stt_stream_fallbacks_total ≥ 1: %s", body)
	}
	if !metricAtLeast(body, "grade_vad_stream_fallbacks_total", 1) {
		t.Fatalf("нет метрики grade_vad_stream_fallbacks_total ≥ 1: %s", body)
	}
}

// TestBargeInSpeechMSBuckets — barge-in несёт метрику grade_barge_in_speech_ms
// (решение #42): длительность речи (totalMS − pre-roll) в бакеты. Детерминированная
// последовательность для /vad/stream (после деградации /stt/stream): 2×тишина
// (pre-ring) + 4×тон 250 мс (1 с речи) + 3×тишина → реплика 2000 мс, pre-roll
// 750 мс (2 тише-кадра + кадр-триггер, race — known limitation) → ms = 1250 ∈
// (1000, 2000]: count +1, le=2000 +1, le=1000 без изменения. Длинная речь
// прерывает TTS (tts_stop + transcript user).
func TestBargeInSpeechMSBuckets(t *testing.T) {
	ts, token, sessionID, m, _ := newVoiceEnv(t)
	m.streamBroken = true // level 1 мёртв → /vad/stream
	m.ttsPCMSize = 96000  // 3 с TTS на реплику ИИ (12 pacing-кадров)
	conn := dialWS(t, ts, token, sessionID)
	defer func() { _ = conn.Close(websocket.StatusNormalClosure, "") }()

	// Старт: stage + приветствие; TTS приветствия — до end-кадра.
	msgs := wsReadMixed(t, conn, 2, 3*time.Second)
	if msgs[0] != "text:stage/<nil>/<nil>" || !startsWith(msgs[1], "text:ai_text/") {
		t.Fatalf("старт: %v", msgs)
	}
	greetingEnd := false
	deadline := time.Now().Add(10 * time.Second)
	for !greetingEnd && time.Now().Before(deadline) {
		typ, _, fr, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" && fr.end {
			greetingEnd = true
		}
	}
	// Текстовая реплика → ход ИИ (pacing, ttsActive=true).
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	_ = conn.Write(ctx, websocket.MessageText, mustJSON(
		map[string]any{"type": "ui", "name": "utterance", "payload": map[string]string{"text": "Привет"}}))
	cancel()
	bins := 0
	deadline = time.Now().Add(5 * time.Second)
	for bins < 2 && time.Now().Before(deadline) {
		typ, _, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "bin" {
			bins++
		}
	}
	if bins < 2 {
		t.Fatal("TTS-кадры ответа ИИ не пришли (pacing не стартовал)")
	}
	writePCM := func(pcm []byte) {
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		_ = conn.Write(ctx, websocket.MessageBinary, pcm)
		cancel()
	}
	// Приминг-кадр: запускает /stt/stream-клиент (мёртв: 3 реконнекта ~1 с).
	// Во время ожидания кадры НЕ шлём — в /vad/stream ничего не «протекает»
	// (детерминизм состава реплики). Ждём ДЕЛЬТУ глобальной метрики (тесты
	// идут последовательно в одном процессе — базовое значение может быть > 0).
	baseFallbacks := metricValue(metricsBody(t, ts.URL), "grade_stt_stream_fallbacks_total")
	writePCM(tonePCM(250, 5000))
	for i := 0; i < 50; i++ {
		if metricValue(metricsBody(t, ts.URL), "grade_stt_stream_fallbacks_total") > baseFallbacks {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	base := metricsBody(t, ts.URL)
	bargeLabel := `result="barge_in"`
	baseCount := histValue(base, "grade_barge_in_speech_ms", bargeLabel, "")
	base1000 := histValue(base, "grade_barge_in_speech_ms", bargeLabel, "1000")
	base2000 := histValue(base, "grade_barge_in_speech_ms", bargeLabel, "2000")
	// Детерминированная реплика: 2×тишина (pre-ring) + 4×тон (1 с речи) + 3×тишина.
	// Кадры с интервалом 50 мс (имитация реального времени): событие VAD-стрима
	// обрабатывается клиентом МЕЖДУ кадрами (при burst-отправке ring «догоняет"
	// кадры — pre-roll завышается до 4 кадров, known limitation).
	for i := 0; i < 2; i++ {
		writePCM(silencePCM(250))
		time.Sleep(50 * time.Millisecond)
	}
	for i := 0; i < 4; i++ {
		writePCM(tonePCM(250, 5000))
		time.Sleep(50 * time.Millisecond)
	}
	for i := 0; i < 3; i++ {
		writePCM(silencePCM(250))
		time.Sleep(50 * time.Millisecond)
	}
	// (1) длинная речь прерывает TTS: tts_stop + transcript(user).
	sawTTSStop, sawUser := false, false
	deadline = time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) && !(sawTTSStop && sawUser) {
		typ, who, _, ok := bargeInReadOne(t, conn)
		if !ok {
			continue
		}
		if typ == "tts_stop" {
			sawTTSStop = true
		}
		if typ == "transcript" && who == "user" {
			sawUser = true
		}
	}
	if !sawTTSStop {
		t.Fatal("нет tts_stop — barge-in по Silero-пути не сработал")
	}
	if !sawUser {
		t.Fatal("нет transcript(user) от прерывающей реплики")
	}
	// (2) метрика: наблюдение 1250 мс ∈ (1000, 2000] — count +1, le=2000 +1,
	// le=1000 без изменения.
	body := metricsBody(t, ts.URL)
	if got := histValue(body, "grade_barge_in_speech_ms", bargeLabel, ""); got != baseCount+1 {
		t.Fatalf("grade_barge_in_speech_ms_count = %d (want %d = база + 1)", got, baseCount+1)
	}
	if got := histValue(body, "grade_barge_in_speech_ms", bargeLabel, "2000"); got != base2000+1 {
		t.Fatalf("bucket le=2000 = %d (want база + 1: ms 1250 ≤ 2000)", got)
	}
	if got := histValue(body, "grade_barge_in_speech_ms", bargeLabel, "1000"); got != base1000 {
		t.Fatalf("bucket le=1000 = %d (want база: ms 1250 > 1000)", got)
	}
}

// TestBargeInSileroNegativeShortSpeech — НЕГАТИВНЫЙ кейс barge-in Silero-пути
// (T-20261009152316): totalMS=800, pre-roll=500 → ms = 300 < BargeInMinSpeechMS=500
// → TTS НЕ прерывается (нет tts_stop, счётчики barge-in не растут), метрика
// несёт ms=300 с меткой result="ignored" (le=500 +1, le=250 без изменения).
// Прямой вызов completeUtterance с синтетической репликой — детерминировано,
// без живых сервисов и WS (кадровая арифметика мока VAD даёт ms ≥ 750:
// post-silence 500 + тон 250 — known limitation, решение #42).
func TestBargeInSileroNegativeShortSpeech(t *testing.T) {
	_, _, _, _, env := newVoiceEnv(t)
	// wsSession напрямую: ИИ говорит (ttsActive), pre-STT активен (проверяем сброс).
	ws := &wsSession{id: -1, ctx: context.Background()}
	ws.ttsActive.Store(true)
	ws.preSTTActive.Store(true)

	base := metricsBody(t, env.ts.URL)
	bargeLabel := `result="barge_in"`
	ignoreLabel := `result="ignored"`
	baseBarge := histValue(base, "grade_barge_in_speech_ms", bargeLabel, "")
	baseTotal := metricValue(base, "grade_barge_ins_total")

	// 800 мс @ 16 кГц 16-bit = 25 600 байт; pre-roll 500 мс → ms = 300.
	utterance := make([]byte, 16000*2*800/1000)
	env.srv.completeUtterance(ws, utterance, 500)

	if !ws.ttsActive.Load() {
		t.Fatal("TTS прервана (ttsActive=false) — короткая речь (300 мс < 500) не должна прерывать")
	}
	if ws.preSTTActive.Load() {
		t.Fatal("preSTTActive не сброшен после завершения реплики")
	}
	body := metricsBody(t, env.ts.URL)
	// Метрика: ms=300 → ignored: count +1, le=500 +1, le=250 без изменения.
	if got := histValue(body, "grade_barge_in_speech_ms", ignoreLabel, ""); got != 1 {
		t.Fatalf("grade_barge_in_speech_ms{result=ignored}_count = %d (want 1)", got)
	}
	if got := histValue(body, "grade_barge_in_speech_ms", ignoreLabel, "500"); got != 1 {
		t.Fatalf("ignored le=500 = %d (want 1: ms 300 ≤ 500)", got)
	}
	if got := histValue(body, "grade_barge_in_speech_ms", ignoreLabel, "250"); got != 0 {
		t.Fatalf("ignored le=250 = %d (want 0: ms 300 > 250)", got)
	}
	// Barge-in НЕ было: серия result="barge_in" и счётчик без изменений.
	if got := histValue(body, "grade_barge_in_speech_ms", bargeLabel, ""); got != baseBarge {
		t.Fatalf("серия result=barge_in изменилась: %d → %d", baseBarge, got)
	}
	if got := metricValue(body, "grade_barge_ins_total"); got != baseTotal {
		t.Fatalf("grade_barge_ins_total: %d → %d (barge-in не должен был произойти)", baseTotal, got)
	}
}

// histValue — значение ряда гистограммы в /metrics-теле: name_bucket{label,le=le}
// (le="" — name{label}_count; label="" — без метки); 0 — нет строки.
// Формат экспорта: bucket-метки после _bucket, прочие — в имени ряда (name{label}_count).
func histValue(body, name, label, le string) int {
	var prefix string
	if le == "" {
		prefix = name
		if label != "" {
			prefix += "{" + label + "}"
		}
		prefix += "_count "
	} else {
		prefix = name + "_bucket{"
		if label != "" {
			prefix += label + ","
		}
		prefix += "le=" + le + "} "
	}
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, prefix) {
			if v, err := strconv.Atoi(strings.TrimSpace(line[len(prefix):])); err == nil {
				return v
			}
		}
	}
	return 0
}

// metricValue — значение счётчика name в /metrics-теле (0 — нет строки).
func metricValue(body, name string) int {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, name+" ") {
			if v, err := strconv.Atoi(strings.TrimSpace(line[len(name)+1:])); err == nil {
				return v
			}
		}
	}
	return 0
}

// metricAtLeast — счётчик name в /metrics-теле ≥ n (метрики моносотные —
// сравниваем порог, а не точное значение: тесты идут последовательно).
func metricAtLeast(body, name string, n int) bool {
	for _, line := range strings.Split(body, "\n") {
		if strings.HasPrefix(line, name+" ") {
			v, err := strconv.Atoi(strings.TrimSpace(line[len(name)+1:]))
			if err == nil && v >= n {
				return true
			}
		}
	}
	return false
}

// metricsBody — GET /metrics (тестовый стенд).
func metricsBody(t *testing.T, apiURL string) string {
	t.Helper()
	resp, err := http.Get(apiURL + "/metrics")
	if err != nil {
		t.Fatalf("metrics: %v", err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	return string(b)
}
