package voiceorchestrator

import (
	"bytes"
	"context"
	"encoding/binary"
	"log/slog"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
	"github.com/stelmakhdigital/grade-iv/services/api/internal/session"
)

// --- фейковые провайдеры для unit-тестов оркестрации (без живых сервисов) ---

// fakeEngine — движок: снимок + запись отправленных событий/кадров.
type fakeEngine struct {
	snapshot *session.Snapshot
	sentTo   []any
	sentBin  [][]byte
}

func (f *fakeEngine) Snapshot(id int64) (*session.Snapshot, error) { return f.snapshot, nil }
func (f *fakeEngine) SendTo(id int64, v any)                       { f.sentTo = append(f.sentTo, v) }
func (f *fakeEngine) SendBinary(id int64, d []byte) {
	f.sentBin = append(f.sentBin, append([]byte(nil), d...))
}

// fakeVoice — TTS: возвращает фиксированный PCM на каждый запрос.
type fakeVoice struct{ pcm []byte }

func (f *fakeVoice) TTS(ctx context.Context, text string) ([]byte, error) {
	return append([]byte(nil), f.pcm...), nil
}

// fakeInterviewer — LLM: стримит фиксированные дельты ответа.
type fakeInterviewer struct{ deltas []string }

func (f *fakeInterviewer) OnUserUtteranceStream(ctx context.Context, id int64, text string) (<-chan string, error) {
	ch := make(chan string, len(f.deltas)+1)
	for _, d := range f.deltas {
		ch <- d
	}
	close(ch)
	return ch, nil
}

// fakeConn — WS-подключение: фиксированный id/ctx, barge-in через c.stop.
type fakeConn struct {
	id   int64
	ctx  context.Context
	stop chan struct{}
}

func (c *fakeConn) ID() int64            { return c.id }
func (c *fakeConn) Ctx() context.Context { return c.ctx }
func (c *fakeConn) SessStopped() bool    { return false }
func (c *fakeConn) BeginTTS() <-chan struct{} {
	c.stop = make(chan struct{})
	return c.stop
}
func (c *fakeConn) EndTTS(stop <-chan struct{}) {}

func newTestOrch(eng *fakeEngine, it *fakeInterviewer) *Orchestrator {
	return New(Deps{
		Engine:              eng,
		Interviewer:         it,
		Log:                 slog.New(slog.DiscardHandler),
		EventData:           func(ctx context.Context, id int64, kind string, data map[string]any) (int, error) { return 0, nil },
		SendInterviewerText: func(ctx context.Context, id int64, result string, err error) {},
	})
}

// hasMessage — есть ли в sentTo сообщение map с type=t и (опц.) who=w / text.
func hasMessage(sent []any, typ, who, text string) bool {
	for _, m := range sent {
		mm, ok := m.(map[string]any)
		if !ok {
			continue
		}
		if mm["type"] != typ {
			continue
		}
		if who != "" && mm["who"] != who {
			continue
		}
		if text != "" && mm["text"] != text {
			continue
		}
		return true
	}
	return false
}

// TestRunCandidateTurn_ProducesTTS — полный ход с фейковыми провайдерами:
// transcript(user) + LLM-стрим → transcript(ai)/ai_text + TTS-кадры (формат ADR-001).
func TestRunCandidateTurn_ProducesTTS(t *testing.T) {
	reply := "Привет, расскажи о себе."
	eng := &fakeEngine{snapshot: &session.Snapshot{Status: models.StatusActive}}
	o := newTestOrch(eng, &fakeInterviewer{deltas: []string{"Привет, ", "расскажи о себе."}})
	// 1 кадр PCM (8000 байт = 250 мс) → 1 аудио-кадр + 1 end-кадр.
	voice := &fakeVoice{pcm: make([]byte, ChunkBytes)}
	c := &fakeConn{id: 7, ctx: context.Background()}

	o.RunCandidateTurn(c, voice, "привет")

	if !hasMessage(eng.sentTo, "transcript", "user", "привет") {
		t.Error("нет transcript(user)")
	}
	if !hasMessage(eng.sentTo, "transcript", "ai", reply) {
		t.Errorf("нет transcript(ai) с LLM-ответом; sentTo=%v", eng.sentTo)
	}
	if !hasMessage(eng.sentTo, "ai_text", "", reply) {
		t.Errorf("нет ai_text с LLM-ответом; sentTo=%v", eng.sentTo)
	}
	// TTS-кадры: {seq u16 LE, flags u16 LE}+PCM (8004 байта); end-кадр с FlagEnd.
	if len(eng.sentBin) < 2 {
		t.Fatalf("мало TTS-кадров: %d (want ≥2: аудио + end)", len(eng.sentBin))
	}
	for i, fr := range eng.sentBin {
		if len(fr) != 4+ChunkBytes {
			t.Fatalf("кадр %d: длина %d, want %d", i, len(fr), 4+ChunkBytes)
		}
	}
	last := eng.sentBin[len(eng.sentBin)-1]
	if binary.LittleEndian.Uint16(last[2:4]) != FlagEnd {
		t.Errorf("последний кадр без end-флага: flags=%d", binary.LittleEndian.Uint16(last[2:4]))
	}
	// seq сквозной: 0,1,2,...
	for i, fr := range eng.sentBin {
		if got := binary.LittleEndian.Uint16(fr[0:2]); got != uint16(i) {
			t.Fatalf("кадр %d: seq=%d, want %d", i, got, i)
		}
	}
}

// TestRunCandidateTurn_NoVoice — без TTS (voice=nil): кадры не идут, но
// transcript/ai_text отправляются (деградация: текст без озвучки).
func TestRunCandidateTurn_NoVoice(t *testing.T) {
	reply := "Ответ без голоса."
	eng := &fakeEngine{snapshot: &session.Snapshot{Status: models.StatusActive}}
	o := newTestOrch(eng, &fakeInterviewer{deltas: []string{reply}})
	c := &fakeConn{id: 7, ctx: context.Background()}

	o.RunCandidateTurn(c, nil, "привет")

	if !hasMessage(eng.sentTo, "transcript", "ai", reply) {
		t.Errorf("нет transcript(ai); sentTo=%v", eng.sentTo)
	}
	if !hasMessage(eng.sentTo, "ai_text", "", reply) {
		t.Errorf("нет ai_text; sentTo=%v", eng.sentTo)
	}
	if len(eng.sentBin) != 0 {
		t.Errorf("без voice не должно быть TTS-кадров, их %d", len(eng.sentBin))
	}
}

// TestRunCandidateTurn_InactiveSession — сессия не активна (пауза) — ход не
// обрабатывается (ни transcript, ни LLM, ни TTS).
func TestRunCandidateTurn_InactiveSession(t *testing.T) {
	eng := &fakeEngine{snapshot: &session.Snapshot{Status: models.StatusPaused}}
	it := &fakeInterviewer{deltas: []string{"должно не вызваться"}}
	o := newTestOrch(eng, it)
	c := &fakeConn{id: 7, ctx: context.Background()}

	o.RunCandidateTurn(c, &fakeVoice{pcm: make([]byte, ChunkBytes)}, "привет")

	if hasMessage(eng.sentTo, "transcript", "user", "привет") {
		t.Error("неактивная сессия не должна давать transcript(user)")
	}
	if len(eng.sentBin) != 0 {
		t.Errorf("неактивная сессия не должна давать TTS-кадры, их %d", len(eng.sentBin))
	}
}

// TestStreamCandidateTurn_BargeIn — barge-in: stop закрыт во время стрима →
// кадры больше не идут, стрим завершается end-кадром (не все кадры отправлены).
func TestStreamCandidateTurn_BargeIn(t *testing.T) {
	eng := &fakeEngine{snapshot: &session.Snapshot{Status: models.StatusActive}}
	o := newTestOrch(eng, &fakeInterviewer{deltas: []string{"Раз два три четыре пять шесть семь восемь девять десять."}})
	// 10 кадров PCM = 2.5 с pacing при отсутствии barge-in.
	voice := &fakeVoice{pcm: make([]byte, ChunkBytes*10)}
	c := &fakeConn{id: 7, ctx: context.Background()}

	// Запустим ход; barge-in через 80 мс (после ~1 кадра).
	done := make(chan struct{})
	go func() {
		o.RunCandidateTurn(c, voice, "привет")
		close(done)
	}()
	// Дождёмся начала TTS (первый кадр) и прервём.
	deadline := time.Now().Add(2 * time.Second)
	for c.stop == nil && time.Now().Before(deadline) {
		time.Sleep(5 * time.Millisecond)
	}
	if c.stop != nil {
		close(c.stop) // barge-in
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("ход не завершился после barge-in (deadlock?)")
	}
	// Barge-in остановил стрим до конца: не все 10 кадров отправлены.
	pcmFrames := 0
	for _, fr := range eng.sentBin {
		if binary.LittleEndian.Uint16(fr[2:4]) == 0 {
			pcmFrames++
		}
	}
	if pcmFrames >= 10 {
		t.Errorf("barge-in не остановил стрим: отправлено %d аудио-кадров (want <10)", pcmFrames)
	}
	// Последнее, что ушло, — end-кадр (гарантия остановки плеера).
	if len(eng.sentBin) > 0 {
		last := eng.sentBin[len(eng.sentBin)-1]
		if binary.LittleEndian.Uint16(last[2:4]) != FlagEnd {
			t.Errorf("после barge-in последний кадр не end-кадр: flags=%d", binary.LittleEndian.Uint16(last[2:4]))
		}
	}
}

var _ = bytes.Equal
