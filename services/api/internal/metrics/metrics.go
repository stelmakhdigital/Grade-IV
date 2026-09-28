// Package metrics — минимальный Prometheus-экспортер (текстовый формат) без
// внешних зависимостей: счётчики и гистограммы, роут /metrics.
package metrics

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
)

// Labels — метки метрики.
type Labels map[string]string

// turnBuckets — buckets гистограммы хода (с); p95 ≈ граница 16/32.
var turnBuckets = []float64{0.5, 1, 2, 4, 8, 16, 32, 64}

// labelKey — каноническое имя ряда (метки по алфавиту).
func labelKey(l Labels) string {
	if len(l) == 0 {
		return ""
	}
	keys := make([]string, 0, len(l))
	for k := range l {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var sb strings.Builder
	for i, k := range keys {
		if i > 0 {
			sb.WriteString(",")
		}
		fmt.Fprintf(&sb, "%s=%q", k, l[k])
	}
	return sb.String()
}

func labeled(name, key string) string {
	if key == "" {
		return name
	}
	return name + "{" + key + "}"
}

// Counter — счётчик с метками.
type Counter struct {
	name string
	help string
	mu   sync.Mutex
	vals map[string]float64
}

func NewCounter(name, help string) *Counter {
	return &Counter{name: name, help: help, vals: map[string]float64{}}
}

func (c *Counter) Add(l Labels, v float64) {
	c.mu.Lock()
	c.vals[labelKey(l)] += v
	c.mu.Unlock()
}

func (c *Counter) Inc(l Labels) { c.Add(l, 1) }

func (c *Counter) text() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	keys := make([]string, 0, len(c.vals))
	for k := range c.vals {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s counter\n", c.name, c.help, c.name)
	for _, k := range keys {
		fmt.Fprintf(&b, "%s %g\n", labeled(c.name, k), c.vals[k])
	}
	return b.String()
}

// series — гистограмма одного ряда.
type series struct {
	counts []float64 // по buckets (накопительно)
	count  float64
	sum    float64
}

// Histogram — гистограмма с фиксированными buckets (секунды).
type Histogram struct {
	name    string
	help    string
	buckets []float64
	mu      sync.Mutex
	series  map[string]*series
}

func NewHistogram(name, help string, buckets ...float64) *Histogram {
	if len(buckets) == 0 {
		buckets = turnBuckets
	}
	sorted := make([]float64, len(buckets))
	copy(sorted, buckets)
	sort.Float64s(sorted)
	return &Histogram{name: name, help: help, buckets: sorted, series: map[string]*series{}}
}

// Observe — наблюдение длительности (секунды).
func (h *Histogram) Observe(l Labels, seconds float64) {
	key := labelKey(l)
	h.mu.Lock()
	s, ok := h.series[key]
	if !ok {
		s = &series{counts: make([]float64, len(h.buckets))}
		h.series[key] = s
	}
	for i, b := range h.buckets {
		if seconds <= b {
			s.counts[i]++
		}
	}
	s.count++
	s.sum += seconds
	h.mu.Unlock()
}

func (h *Histogram) text() string {
	h.mu.Lock()
	defer h.mu.Unlock()
	keys := make([]string, 0, len(h.series))
	for k := range h.series {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	var b strings.Builder
	fmt.Fprintf(&b, "# HELP %s %s\n# TYPE %s histogram\n", h.name, h.help, h.name)
	for _, k := range keys {
		s := h.series[k]
		base := labeled(h.name, k)
		for i, bkt := range h.buckets {
			le := k
			if le == "" {
				le = fmt.Sprintf("le=%g", bkt)
			} else {
				le = k + ",le=" + fmt.Sprintf("%g", bkt)
			}
			fmt.Fprintf(&b, "%s_bucket{%s} %g\n", h.name, le, s.counts[i])
		}
		fmt.Fprintf(&b, "%s_sum %g\n", base, s.sum)
		fmt.Fprintf(&b, "%s_count %g\n", base, s.count)
	}
	return b.String()
}

// Метрики хода голосового интервью (стages: turn_start/llm_first_token/
// tts_first_frame/turn_end — время от начала хода до события, с).
var (
	TurnStage = NewHistogram("grade_ai_turn_seconds",
		"время (с) от начала хода кандидата до события стадии")
	// TurnsTotal — завершённые ходы по результату (result=ok|error|no_audio).
	TurnsTotal = NewCounter("grade_ai_turns_total", "ходов кандидата по результату")
	// LLMStreamErrors — ошибки запроса/соединения стрима LLM.
	LLMStreamErrors = NewCounter("grade_ai_llm_stream_errors_total", "ошибок запроса/соединения LLM-стрима")
	// TTSErrors — ошибок синтеза предложения (TTS).
	TTSErrors = NewCounter("grade_ai_tts_errors_total", "ошибок синтеза TTS")
)

// StageLabel — метка стадии хода.
func StageLabel(stage string) Labels { return Labels{"stage": stage} }

// ResultLabel — метка результата хода.
func ResultLabel(result string) Labels { return Labels{"result": result} }

type texter interface{ text() string }

var all = []texter{TurnStage, TurnsTotal, LLMStreamErrors, TTSErrors}

// Handler — GET /metrics (Prometheus text format).
func Handler(w http.ResponseWriter, _ *http.Request) {
	var b strings.Builder
	for _, m := range all {
		b.WriteString(m.text())
	}
	w.Header().Set("Content-Type", "text/plain; version=0.0.4; charset=utf-8")
	_, _ = w.Write([]byte(b.String()))
}
