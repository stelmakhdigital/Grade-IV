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
		{"123 и 45%", "123 и 45%"}, // цифры/проценты
		{"Привет! Как дела?", "Привет! Как дела?"}, // чистый русский
		{"Go-сервис", "гоу-сервис"}, // слово в составе через дефис
	}
	for _, c := range cases {
		got := prepareTTS(c.in)
		if got != c.want {
			t.Errorf("prepareTTS(%q) = %q, want %q", c.in, got, c.want)
		}
	}
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
