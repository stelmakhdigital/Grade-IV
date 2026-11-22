// Package resume — текст резюме из приложенного файла (docx/pdf).
//
// Файл нужен ИИ-интервьюеру, а не архиву: LLM текстовый, поэтому из pdf/docx
// вытаскивается текст. docx — zip с word/document.xml (stdlib), pdf — чистая
// Go-либа. Legacy .doc (бинарный OLE) не разбирается: файл храним, текст — пустой.
package resume

import (
	"archive/zip"
	"bytes"
	"fmt"
	"html"
	"io"
	"regexp"
	"strings"

	"github.com/ledongthuc/pdf"
)

// MaxBytes — лимит размера файла резюме (8 МБ: типичное резюме — 100–500 КБ).
const MaxBytes = 8 << 20

// MaxTextRunes — лимит извлечённого текста (в промпт уходит не всё, и в БД
// бесконечный текст не нужен).
const MaxTextRunes = 40000

// Supported — известное расширение резюме (по суффиксу имени файла).
// 'doc' принимается, но не разбирается (см. Extract).
func Supported(name string) bool {
	switch ext(name) {
	case "pdf", "docx", "doc":
		return true
	}
	return false
}

// ext — нижний регистр расширения без точки.
func ext(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i < 0 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

// Extract — текст из файла резюме. Пустая строка без ошибки — формат принят,
// но текста нет (.doc, отсканированный PDF): кандидат просит добавить описание.
func Extract(name string, data []byte) (string, error) {
	switch ext(name) {
	case "docx":
		return docxText(data)
	case "pdf":
		return pdfText(data)
	case "doc":
		return "", nil
	default:
		return "", fmt.Errorf("формат %q не поддерживается (pdf, docx, doc)", ext(name))
	}
}

// docxText — текст из word/document.xml: абзацы по </w:p>, теги снимаются.
func docxText(data []byte) (string, error) {
	zr, err := zip.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("docx: %w", err)
	}
	var xml []byte
	for _, f := range zr.File {
		if f.Name != "word/document.xml" {
			continue
		}
		rc, err := f.Open()
		if err != nil {
			return "", fmt.Errorf("docx: document.xml: %w", err)
		}
		xml, err = io.ReadAll(io.LimitReader(rc, 32<<20))
		rc.Close()
		if err != nil {
			return "", fmt.Errorf("docx: чтение: %w", err)
		}
		break
	}
	if xml == nil {
		return "", fmt.Errorf("docx: word/document.xml не найден")
	}
	return stripTags(string(xml)), nil
}

var (
	tagRe    = regexp.MustCompile(`<[^>]*>`)
	spaceRe  = regexp.MustCompile(`[ \t]+`)
	blankRe  = regexp.MustCompile(`\n{3,}`)
	newlineP = regexp.MustCompile(`</w:p>`)
)

// stripTags — текст узлов XML без разметки и с разбор сущностей.
func stripTags(s string) string {
	s = newlineP.ReplaceAllString(s, "\n")
	s = strings.NewReplacer("<w:tab/>", "\t", "<w:br/>", "\n").Replace(s)
	s = tagRe.ReplaceAllString(s, "")
	s = html.UnescapeString(s)
	s = spaceRe.ReplaceAllString(s, " ")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// pdfText — текст PDF. Либа падает паникой на битых/зашифрованных файлах
// (файл даёт кандидат), поэтому recover: паника → ошибка, сервер жив.
func pdfText(data []byte) (text string, err error) {
	defer func() {
		if r := recover(); r != nil {
			text, err = "", fmt.Errorf("pdf: разбор не удался: %v", r)
		}
	}()
	if !bytes.HasPrefix(data, []byte("%PDF-")) {
		return "", fmt.Errorf("pdf: нет заголовка %%PDF")
	}
	r, err := pdf.NewReader(bytes.NewReader(data), int64(len(data)))
	if err != nil {
		return "", fmt.Errorf("pdf: %w", err)
	}
	rd, err := r.GetPlainText()
	if err != nil {
		return "", fmt.Errorf("pdf: текст: %w", err)
	}
	b, err := io.ReadAll(rd)
	if err != nil {
		return "", fmt.Errorf("pdf: чтение: %w", err)
	}
	return normalizePlain(string(b)), nil
}

// normalizePlain — нормализация «сырого» текста PDF (либа не расставляет абзацы).
func normalizePlain(s string) string {
	s = strings.NewReplacer("\r", "\n", "\x00", " ").Replace(s)
	s = spaceRe.ReplaceAllString(s, " ")
	lines := strings.Split(s, "\n")
	for i, ln := range lines {
		lines[i] = strings.TrimSpace(ln)
	}
	s = strings.Join(lines, "\n")
	s = blankRe.ReplaceAllString(s, "\n\n")
	return strings.TrimSpace(s)
}

// Clamp — обрезка по лимиту текста (промпт/БД), с маркером обрезки.
func Clamp(s string) string {
	r := []rune(s)
	if len(r) <= MaxTextRunes {
		return s
	}
	return string(r[:MaxTextRunes]) + "\n…[обрезано]"
}
