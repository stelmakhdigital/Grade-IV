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
	RMSThreshold int // абсолютный минимум порога «речь есть» по RMS int16
	// (default 100; ~0.003 amplitude). Фактический порог адаптивный:
	// max(RMSThreshold, NoiseFloorGain × noise-floor) — см. Adaptive().
	// NoiseFloorGain — множитель шумового пола (default 3): порог «речь» =
	// 3× текущего уровня шума (фон комнаты, дыхание, клавиатура). Защита
	// от тихих микрофонов: фиксированный порог 500 не слышал тихую речь,
	// а шум/дыхание проходили как «реплики».
	NoiseFloorGain float64
	// PreSilenceMS — «предварительная тишина»: тишина ≥ порога ПОСЛЕ речи,
	// но ещё до EndSilenceMS (default 400). Сигнал для pre-STT: распознавание
	// можно запустить на текущем буфере реплики — оно перекрывает остаток
	// VAD-хвоста и экономит время STT в пайплайне (~0.7 с на CPU).
	PreSilenceMS int
}

// DefaultConfig — пороки по умолчанию.
func DefaultConfig() Config {
	return Config{
		SampleRate:   16000,
		EndSilenceMS: 900,
		MinSpeechMS:  400,
		MaxSpeechMS:  20000,
		RMSThreshold:   100,
		NoiseFloorGain: 3,
		PreSilenceMS:   400,
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

	// noiseFloor — оценка фона шума (EMA: быстро следует за снижением,
	// медленно за повышением, чтобы речь не «подняла» пол).
	noiseFloor  float64
	noiseInit   bool
	noiseFrames int
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
		cfg.RMSThreshold = 100
	}
	if cfg.NoiseFloorGain <= 0 {
		cfg.NoiseFloorGain = 3
	}
	if cfg.PreSilenceMS <= 0 {
		cfg.PreSilenceMS = 400
	}
	if cfg.PreSilenceMS > cfg.EndSilenceMS {
		cfg.PreSilenceMS = cfg.EndSilenceMS / 2
	}
	return &Detector{cfg: cfg}
}

// InSpeech — идёт речь прямо сейчас (для UI/анти-nudge).
func (d *Detector) InSpeech() bool { return d.inSpeech }

// NoiseFloor — текущая оценка фона шума (RMS, для отладки/UI).
func (d *Detector) NoiseFloor() float64 { return d.noiseFloor }

// Adaptive — фактический порог «речь есть»: абсолютный минимум + адаптивный
// шумовой пол (gain × floor). Первые кадры (пол не оценён) — только минимум.
func (d *Detector) Adaptive() int {
	if !d.noiseInit {
		return d.cfg.RMSThreshold
	}
	thr := float64(d.cfg.RMSThreshold)
	if nf := d.noiseFloor * d.cfg.NoiseFloorGain; nf > thr {
		thr = nf
	}
	if thr > 32767 {
		thr = 32767
	}
	return int(thr)
}

// updateNoise — EMA шумового пола: на «тихом» кадре пол быстро падает к уровню
// кадра, медленно (α=0.05) растёт. Заводится после 20 тихих кадров (~5 с).
func (d *Detector) updateNoise(rms float64, quiet bool) {
	if quiet {
		if d.noiseFrames > 0 {
			if rms < d.noiseFloor {
				d.noiseFloor = rms // мгновенно вниз (шум утих)
			} else {
				d.noiseFloor += 0.05 * (rms - d.noiseFloor) // медленно вверх
			}
		}
		d.noiseFrames++
		if d.noiseFrames >= 20 {
			d.noiseInit = true
		}
	}
}

// PreSilence — предварительная тишина: реплику можно предварительного
// распознать (pre-STT) — речь уже ≥ 400 мс тишины, но EndSilenceMS ещё не
// набрано (кандидат может продолжить). Инвалидация при новом speech-кадре.
func (d *Detector) PreSilence() bool {
	return d.inSpeech && d.silentMS >= d.cfg.PreSilenceMS
}

// UtteranceLen — текущая длина накопленного аудио реплики (в байтах).
// Меняется только на speech-кадрах; тишина в реплику не входит.
func (d *Detector) UtteranceLen() int { return len(d.utterance) }

// Utterance — копия накопленного аудио реплики (для pre-STT: распознать
// текущий буфер, пока VAD-хвост ещё не завершился).
func (d *Detector) Utterance() []byte {
	if len(d.utterance) == 0 {
		return nil
	}
	buf := make([]byte, len(d.utterance))
	copy(buf, d.utterance)
	return buf
}

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
	thr := float64(d.Adaptive())
	if rms >= thr {
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
	d.updateNoise(rms, true)
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
