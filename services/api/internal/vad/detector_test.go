package vad

import (
	"math"
	"testing"
)

// toneFrame — кадр «речи»: синусоида с амплитудой amp (int16).
func toneFrame(ms, amp int) []byte {
	n := 16000 * ms / 1000
	pcm := make([]byte, n*2)
	for i := 0; i < n; i++ {
		s := int16(float64(amp) * math.Sin(2*math.Pi*440*float64(i)/16000))
		pcm[2*i] = byte(s)
		pcm[2*i+1] = byte(s >> 8)
	}
	return pcm
}

// silenceFrame — кадр тишины (тихий шум с амплитудой 10).
func silenceFrame(ms int) []byte { return toneFrame(ms, 10) }

func newTestDetector() *Detector {
	return New(Config{SampleRate: 16000, EndSilenceMS: 900, MinSpeechMS: 400, MaxSpeechMS: 20000, RMSThreshold: 500})
}

func TestVADSilenceOnly(t *testing.T) {
	d := newTestDetector()
	for i := 0; i < 10; i++ {
		if u, ok := d.Feed(silenceFrame(250)); ok || u != nil {
			t.Fatalf("тишина не должна давать реплику (кадр %d)", i)
		}
	}
}

func TestVADUtteranceAfterSilenceTail(t *testing.T) {
	d := newTestDetector()
	// 1 с речи (4 кадра по 250 мс), затем тишина.
	var u []byte
	var ok bool
	for i := 0; i < 4; i++ {
		u, ok = d.Feed(toneFrame(250, 5000))
		if ok {
			t.Fatalf("реплика не должна завершиться на %d-м кадре речи", i)
		}
	}
	// 900 мс тишины = 4 кадра по 250 мс (1000 мс >= 900) → завершение на 4-м.
	for i := 1; i <= 4; i++ {
		u, ok = d.Feed(silenceFrame(250))
		if i < 4 {
			if ok {
				t.Fatalf("слишком раннее завершение на тишине %d (250*%d мс)", i, i)
			}
			continue
		}
		if !ok {
			t.Fatalf("реплика не завершена после 1 с тишины")
		}
	}
	// Аудио: 1 с речи (4 кадра × 8000 байт = 32000), хвост тишины не включается.
	if len(u) != 32000 {
		t.Fatalf("длина реплики: %d (ожидалось %d)", len(u), 32000)
	}
}

func TestVADShortBurstDropped(t *testing.T) {
	d := newTestDetector()
	// Всплеск 200 мс (< MinSpeechMS=400) + тишина → отбрасывается.
	if _, ok := d.Feed(toneFrame(200, 5000)); ok {
		t.Fatal("короткий всплеск не завершён до тишины — ок, но дальше проверим")
	}
	var ok bool
	for i := 0; i < 5; i++ {
		if _, ok = d.Feed(silenceFrame(250)); ok {
			t.Fatal("короткий всплеск (<400 мс) не должен стать репликой")
		}
	}
}

func TestVADMaxSpeechCut(t *testing.T) {
	d := New(Config{SampleRate: 16000, EndSilenceMS: 900, MinSpeechMS: 400, MaxSpeechMS: 1000, RMSThreshold: 500})
	// 1000 мс речи = MaxSpeechMS → принудительный срез.
	var u []byte
	var ok bool
	for i := 0; i < 4; i++ {
		u, ok = d.Feed(toneFrame(250, 5000))
		if ok {
			break
		}
	}
	if !ok {
		t.Fatal("должен быть принудительный срез на MaxSpeechMS")
	}
	if len(u) != 32000 {
		t.Fatalf("длина среза: %d (ожидалось %d)", len(u), 32000)
	}
}

func TestVADPauseInsideUtterance(t *testing.T) {
	d := newTestDetector()
	// Речь 0.5 с, пауза 0.5 с (< 900), речь ещё 0.5 с → одна реплика ~1.5 с.
	for i := 0; i < 2; i++ {
		d.Feed(toneFrame(250, 5000))
	}
	for i := 0; i < 2; i++ {
		d.Feed(silenceFrame(250))
	}
	for i := 0; i < 2; i++ {
		d.Feed(toneFrame(250, 5000))
	}
	// Теперь тишина до конца.
	var u []byte
	var ok bool
	for i := 0; i < 5; i++ {
		u, ok = d.Feed(silenceFrame(250))
		if ok {
			break
		}
	}
	if !ok {
		t.Fatal("реплика не завершена")
	}
	// 1.5 с речи (32000/2/16000*1000=1500 мс) + хвост тишины не в Audio.
	gotMS := int(int64(len(u)) / 2 / 16000 * 1000)
	if gotMS < 1000 || gotMS > 1600 {
		t.Fatalf("длина: %d мс (ожидалось ~1500, без хвоста)", gotMS)
	}
}

func TestRMSPCM16(t *testing.T) {
	if RMSPCM16(silenceFrame(250)) > 50 {
		t.Fatal("тишина: RMS слишком высок")
	}
	if RMSPCM16(toneFrame(250, 5000)) < 3000 {
		t.Fatal("тон: RMS слишком низок")
	}
}
