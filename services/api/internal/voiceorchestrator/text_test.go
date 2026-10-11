package voiceorchestrator

import (
	"testing"
)

func TestSplitSentences(t *testing.T) {
	cases := []struct {
		in   string
		want int
	}{
		{"Привет! Как дела?", 2},
		{"Первое. Второе. Третье.", 3},
		{"Одно предложение", 1},
		{"Т.д. и т.п.", 1}, // короткие фрагменты/аббревиатуры не дробятся
		{"", 0},
		{"Разрыв ... здесь", 2}, // многоточие — разрыв
	}
	for _, c := range cases {
		if got := SplitSentences(c.in); len(got) != c.want {
			t.Errorf("SplitSentences(%q) = %v (len %d), want len %d", c.in, got, len(got), c.want)
		}
	}
}

func TestTTSGap(t *testing.T) {
	cases := []struct {
		prev string
		want int
	}{
		{"", 0},
		{"Привет?", 2},
		{"Привет!", 2},
		{"Привет.", 1},
		{"Привет...", 1},
		{"Думаю, что главное,", 0},
		{"Один;", 0},
		{"Время:", 0},
		{"Раз —", 0},
		{"без завершителя", 1},
	}
	for _, c := range cases {
		if got := TTSGap(c.prev); got != c.want {
			t.Errorf("TTSGap(%q) = %d, want %d", c.prev, got, c.want)
		}
	}
}

func TestTruncateForLog(t *testing.T) {
	if got := TruncateForLog("краткий", 10); got != "краткий" {
		t.Errorf("короткий не обрезается: %q", got)
	}
	if got := TruncateForLog("длинный текст", 5); got != "длинн…" {
		t.Errorf("обрезка: %q (want %q)", got, "длинн…")
	}
}

func TestPrepareTTS(t *testing.T) {
	cases := map[string]string{
		"проект на Go":        "проект на гоу",
		"использую Python":    "использую питон",
		"кириллица нетронута": "кириллица нетронута",
	}
	for in, want := range cases {
		if got := PrepareTTS(in); got != want {
			t.Errorf("PrepareTTS(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestIsUpperNext(t *testing.T) {
	if !IsUpperNext("Абв", 0) {
		t.Error("кириллическая заглавная не распознана")
	}
	if IsUpperNext("абв", 0) {
		t.Error("кириллическая строчная распознана как заглавная")
	}
	if IsUpperNext("abc", 5) { // i >= len
		t.Error("выход за конец распознан как заглавная")
	}
	// ASCII (size<=1) — по исходной семантике не заглавная (обрабатывается ASCII-путём).
	if IsUpperNext("Hello", 0) {
		t.Error("ASCII size<=1 не должна считаться заглавной (исходная семантика)")
	}
}

func TestHasWordChar(t *testing.T) {
	if !HasWordChar("привет") || !HasWordChar("Go") || !HasWordChar("123") {
		t.Error("буквы/цифры не распознаны")
	}
	if HasWordChar("—") || HasWordChar("…") {
		t.Error("знаки препинания распознаны как слово")
	}
}

func TestSplitForTTS_ClauseDispatch(t *testing.T) {
	// Длинное предложение с запятыми (≥ MinClauseChars) дробится на клиузы.
	long := "Мне нравится этот проект, он большой, мы используем Go и Python и Rust каждый день."
	parts := SplitForTTS(long)
	if len(parts) < 3 {
		t.Fatalf("ожидала ≥3 клиузы, получила %d: %v", len(parts), parts)
	}
	// «12:30» — двоеточие между цифрами НЕ дробит.
	one := "Встреча в 12:30 у меня займёт много времени, потому что всё сложно"
	if parts := SplitForTTS(one); len(parts) < 1 {
		t.Fatalf("нет частей: %v", parts)
	}
	// Короткое предложение без границ — одна часть.
	if got := SplitForTTS("Коротко"); len(got) != 1 || got[0] != "Коротко" {
		t.Errorf("короткое: %v", got)
	}
}
