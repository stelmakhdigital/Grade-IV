package voiceorchestrator

import "testing"

// TestBargeInDecision — чистое решение о barge-in (порог BargeInMinSpeechMS=500 мс,
// pre-roll вычитается из общей длительности речи).
func TestBargeInDecision(t *testing.T) {
	cases := []struct {
		name            string
		ttsActive       bool
		speechMS, preMS int
		wantAction      BargeInAction
		wantMS          int
	}{
		{"tts off — none", false, 1000, 0, BargeInNone, 0},
		{"нет речи — none", true, 0, 0, BargeInNone, 0},
		// Негативный (из integration): 800 мс всего, pre-roll 500 → ms=300 < 500 → ignore.
		{"короткая речь — ignore", true, 800, 500, BargeInIgnore, 300},
		// Достаточная речь: 1000 − 200 = 800 ≥ 500 → trigger.
		{"длинная речь — trigger", true, 1000, 200, BargeInTrigger, 800},
		// pre-roll больше речи → клампится, ms=0 → ignore.
		{"pre-roll > speech — ignore", true, 1000, 2000, BargeInIgnore, 0},
		// Граница: ровно 500 мс → trigger (ms < 500 ложно).
		{"граница 500 — trigger", true, 1000, 500, BargeInTrigger, 500},
		{"без pre-roll 600 — trigger", true, 600, 0, BargeInTrigger, 600},
	}
	for _, c := range cases {
		action, ms := BargeInDecision(c.ttsActive, c.speechMS, c.preMS)
		if action != c.wantAction || ms != c.wantMS {
			t.Errorf("%s: BargeInDecision(%v,%d,%d) = (%v,%d), want (%v,%d)",
				c.name, c.ttsActive, c.speechMS, c.preMS, action, ms, c.wantAction, c.wantMS)
		}
	}
}
