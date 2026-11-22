package resume

import (
	"archive/zip"
	"bytes"
	"os"
	"strings"
	"testing"
)

// docx-in-test: zip с word/document.xml (как его пишет Word).
func TestExtractDocx(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	w, err := zw.Create("word/document.xml")
	if err != nil {
		t.Fatal(err)
	}
	w.Write([]byte(`<?xml version="1.0"?><w:document><w:body>` +
		`<w:p><w:r><w:t>Иван Петров</w:t></w:r></w:p>` +
		`<w:p><w:r><w:t>Go &amp; Python, 5 лет (&quot;middle+&quot;)</w:t></w:r></w:p>` +
		`</w:body></w:document>`))
	zw.Close()

	text, err := Extract("resume.docx", buf.Bytes())
	if err != nil {
		t.Fatalf("Extract: %v", err)
	}
	if !strings.Contains(text, "Иван Петров") || !strings.Contains(text, "Go & Python") {
		t.Fatalf("текст не извлечён: %q", text)
	}
	if strings.Contains(text, "<w:") || strings.Contains(text, "&amp;") {
		t.Fatalf("разметка/сущности не сняты: %q", text)
	}
}

func TestExtractPdf(t *testing.T) {
	// fixture — минимальный PDF с текстовым потоком (создан вручную, xref верный).
	data, err := os.ReadFile("testdata/resume.pdf")
	if err != nil {
		t.Fatalf("fixture: %v", err)
	}
	text, err := Extract("resume.pdf", data)
	if err != nil {
		t.Fatalf("Extract pdf: %v", err)
	}
	if !strings.Contains(text, "Ivan Petrov") {
		t.Fatalf("текст не извлечён: %q", text)
	}
}

// Битые/чужие файлы — ошибка или пусто, но не паника (файл даёт кандидат).
func TestExtractGarbageNoPanic(t *testing.T) {
	for _, name := range []string{"a.pdf", "a.docx", "a.doc", "a.txt"} {
		got, err := Extract(name, []byte("не файл совсем"))
		if name == "a.doc" && err != nil {
			t.Fatalf("doc должен храниться без разбора, got err %v", err)
		}
		if name != "a.doc" && err == nil {
			t.Fatalf("%s: ожидалась ошибка, got %q", name, got)
		}
	}
	if Supported("a.exe") {
		t.Fatal(".exe не должен приниматься")
	}
	if !Supported("Резюме.PDF") {
		t.Fatal("расширение должно проверяться без учёта регистра")
	}
}

func TestClamp(t *testing.T) {
	long := strings.Repeat("ё", MaxTextRunes+10)
	suffix := "\n…[обрезано]"
	if got := Clamp(long); len([]rune(got)) != MaxTextRunes+len([]rune(suffix)) {
		t.Fatalf("обрезка не по лимиту: %d", len([]rune(got)))
	}
	if Clamp("коротко") != "коротко" {
		t.Fatal("короткий текст не должен меняться")
	}
}
