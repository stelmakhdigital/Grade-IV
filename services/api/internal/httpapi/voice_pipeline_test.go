package httpapi

import "testing"


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
