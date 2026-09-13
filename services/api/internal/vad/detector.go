// Package vad — энергетический VAD реплик кандидата (ADR-002: VAD в оркестраторе api).
//
// Упрощение MVP: порог по RMS PCM16 (без onnx-модели в Go). Конец реплики — тишина
// EndSilenceMS после речи; короткие всплески (< MinSpeechMS) отбрасываются;
// длинная речь (>= MaxSpeechMS) принудительно срезается.
// Точная модель (Silero VAD onnx) — бэклог (в voice-сервисе VAD-фильтр уже есть).
package vad

import (
	"math"
)

// Config — параметры детектора.
type Config struct {
	SampleRate   int // 16000 (контракт)
	EndSilenceMS int // тишина после речи → конец реплики (default 900, ADR-002: 700–1200)
	MinSpeechMS  int // короче — всплеск/шум, не реплика (default 400)
	MaxSpeechMS  int // длиннее — принудительный срез (default 20000)
	RMSThreshold int // порог «речь есть» по RMS int16 (default 500; ~0.015 amplitude)
}

// DefaultConfig — пороки по умолчанию.
func DefaultConfig() Config {
	return Config{
		SampleRate:   16000,
		EndSilenceMS: 900,
		MinSpeechMS:  400,
		MaxSpeechMS:  20000,
		RMSThreshold: 500,
	}
}

// Detector — состояние на соединение (не goroutine-safe: опрашивается read-loop).
type Detector struct {
	cfg Config

	inSpeech  bool
	speechMS  int    // длительность текущей речи (от начала)
	silentMS  int    // тишина после начала речи (хвост)
	utterance []byte // накопленное аудио текущей реплики
	frames    int
}

// New создаёт детектор.
func New(cfg Config) *Detector {
	if cfg.SampleRate <= 0 {
		cfg.SampleRate = 16000
	}
	if cfg.EndSilenceMS <= 0 {
		cfg.EndSilenceMS = 900
	}
	if cfg.MinSpeechMS <= 0 {
		cfg.MinSpeechMS = 400
	}
	if cfg.MaxSpeechMS <= 0 {
		cfg.MaxSpeechMS = 20000
	}
	if cfg.RMSThreshold <= 0 {
		cfg.RMSThreshold = 500
	}
	return &Detector{cfg: cfg}
}

// InSpeech — идёт речь прямо сейчас (для UI/анти-nudge).
func (d *Detector) InSpeech() bool { return d.inSpeech }

// FrameMS — длительность кадра в мс.
func (d *Detector) FrameMS(pcm []byte) int {
	if d.cfg.SampleRate == 0 || len(pcm) < 2 {
		return 0
	}
	return int(int64(len(pcm)) * 1000 / 2 / int64(d.cfg.SampleRate))
}

// Feed — кадр PCM16 16 кГц mono. Возвращает завершённую реплику (или nil).
func (d *Detector) Feed(pcm []byte) (utterance []byte, completed bool) {
	frameMS := d.FrameMS(pcm)
	if frameMS <= 0 {
		return nil, false
	}
	d.frames++
	rms := RMSPCM16(pcm)
	if rms >= float64(d.cfg.RMSThreshold) {
		// Кадр речи.
		if !d.inSpeech {
			d.inSpeech = true
			d.speechMS = 0
			d.silentMS = 0
			d.utterance = nil
		}
		d.utterance = append(d.utterance, pcm...)
		d.speechMS += frameMS
		// Принудительный срез очень длинной речи.
		if d.speechMS >= d.cfg.MaxSpeechMS {
			return d.finish()
		}
		return nil, false
	}
	// Кадр тишины.
	if !d.inSpeech {
		return nil, false
	}
	d.silentMS += frameMS
	if d.silentMS < d.cfg.EndSilenceMS {
		return nil, false // ждём: возможно, кандидат просто сделал паузу
	}
	// Тишина после речи достаточна — реплика завершена.
	if d.speechMS < d.cfg.MinSpeechMS {
		d.reset()
		return nil, false // короткий всплеск — не реплика
	}
	return d.finish()
}

// finish — завершить и вернуть накопленное аудио.
func (d *Detector) finish() (utterance []byte, completed bool) {
	u := d.utterance
	d.reset()
	return u, true
}

func (d *Detector) reset() {
	d.inSpeech = false
	d.speechMS = 0
	d.silentMS = 0
	d.utterance = nil
}

// RMSPCM16 — среднеквадратичный уровень кадра (int16 → float, 0..32767).
func RMSPCM16(pcm []byte) float64 {
	if len(pcm) < 2 {
		return 0
	}
	n := len(pcm) / 2
	sum := 0.0
	for i := 0; i < n; i++ {
		s := int16(pcm[2*i]) | int16(pcm[2*i+1])<<8
		sum += float64(s) * float64(s)
	}
	return math.Sqrt(sum / float64(n))
}
