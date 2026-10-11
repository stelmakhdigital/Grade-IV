package voiceorchestrator

import (
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Конвейерные константы (8 kHz mono s16le, клиент берёт первый кадр за ~250 мс).
const (
	// ChunkBytes — TTS-кадр: 250 мс @ 8 kHz mono s16le = 8000 байт PCM (+4 заголовок).
	ChunkBytes = 8000

	// FlagEnd — флаг последнего TTS-кадра (последний байт заголовка).
	FlagEnd = 0x01

	// FrameDur — длительность TTS-кадра (для pacing).
	FrameDur = 250 * time.Millisecond

	// BargeInMinSpeechMS — мин. длительность речи кандидата (мс) для barge-in.
	BargeInMinSpeechMS = 500

	// TTSParallelism — параллельный синтез TTS-фрагментов (ограничит
	// voice-сервис — один процесс).
	TTSParallelism = 3

	// MinClauseChars — мин. длина клиаза для клиуза-уровневой диспетчизации (TTS).
	MinClauseChars = 48
)

// SplitSentences — дробит текст на фразы по завершателям предложений
// («.», «?», «!», «...»). Возвращает непустые, обрезанные фразы.
func SplitSentences(text string) []string {
	parts, tail := SplitSentencesTail(text)
	if p := strings.TrimSpace(tail); p != "" {
		parts = append(parts, p)
	}
	return parts
}

// SplitSentencesTail — то же, что SplitSentences, но возвращает также ОТКРЫТЫЙ
// хвост — текст после последней границы БЕЗ обрезки (raw).
func SplitSentencesTail(text string) ([]string, string) {
	if strings.TrimSpace(text) == "" {
		return nil, text // только пробелы — хвост как есть
	}
	var parts []string
	start := 0
	flush := func(end int) {
		part := strings.TrimSpace(text[start:end])
		if part != "" {
			parts = append(parts, part)
		}
		start = end
	}
	for i := 0; i < len(text); i++ {
		// Многоточие «...» — разрыв.
		if i+2 < len(text) && text[i] == '.' && text[i+1] == '.' && text[i+2] == '.' {
			flush(i + 3)
			i += 2
			continue
		}
		boundary := false
		switch text[i] {
		case '!', '?':
			boundary = true
		case '.':
			// «.» — только перед заглавной (новое предложение) или в конце.
			j := i + 1
			for j < len(text) && (text[j] == ' ' || text[j] == '\n' || text[j] == '\t') {
				j++
			}
			boundary = j >= len(text) || IsUpperNext(text, j)
		}
		if boundary && (i+1 >= len(text) || text[i+1] == ' ' || text[i+1] == '\n' || text[i+1] == '\t') {
			flush(i + 1)
		}
	}
	return parts, text[start:]
}

// SplitForTTS — разбивка на части для TTS-диспетчизации: предложения
// (SplitSentences) + дополнительно клиузы длинных частей (≥ MinClauseChars)
// по границам «,» «;» «:» «—». Короткие предложения не дробятся.
func SplitForTTS(text string) []string {
	parts, tail := SplitForTTSStreaming(text)
	if p := strings.TrimSpace(tail); p != "" {
		parts = append(parts, p)
	}
	return parts
}

// SplitForTTSStreaming — text → ЗАКРЫТЫЕ части для TTS + открытый хвост (raw,
// без обрезки). Все предложения в sents закрыты (дробим в клиузы); открытый
// хвост (tailSent) — тоже дробим, но последняя открытая клиуза остаётся хвостом.
func SplitForTTSStreaming(text string) ([]string, string) {
	sents, tailSent := SplitSentencesTail(text)
	var parts []string
	for _, s := range sents {
		cl, tail := SplitClausesWithTail(s)
		// Завершитель предложения не является границей клиуз — последний
		// клиуз закрытого предложения добирается из хвоста.
		if p := strings.TrimSpace(tail); p != "" {
			cl = append(cl, p)
		}
		if len(cl) == 0 {
			cl = []string{s} // границ не было — предложение целиком
		}
		parts = append(parts, cl...)
	}
	cl, tail := SplitClausesWithTail(tailSent)
	parts = append(parts, cl...)
	return parts, tail
}

// SplitClauses — делит часть (предложение или открытую последнюю) на клиузы
// по «,» «;» «:» «—», если длина ≥ MinClauseChars. «:» внутри чисел («12:30»)
// не дробит. Часть без границ — как есть (одна часть).
func SplitClauses(part string) []string {
	out, tail := SplitClausesWithTail(part)
	if p := strings.TrimSpace(tail); p != "" {
		out = append(out, p)
	}
	return out
}

// SplitClausesWithTail — то же, что SplitClauses, но возвращает закрытые
// клиузы (без последней открытой) + открытый хвост (raw, без обрезки).
func SplitClausesWithTail(part string) ([]string, string) {
	if part == "" {
		return nil, ""
	}
	if len(part) < MinClauseChars {
		return nil, part
	}
	var out []string
	start := 0
	for i := 0; i < len(part); i++ {
		r, size := utf8.DecodeRuneInString(part[i:])
		isColon := false
		if r == ':' {
			// «12:30» не дробим: «:» между цифрами — часть числа.
			digitBefore := i > 0 && part[i-1] >= '0' && part[i-1] <= '9'
			digitAfter := i+size < len(part) && part[i+size] >= '0' && part[i+size] <= '9'
			isColon = !(digitBefore && digitAfter)
		}
		if r == ',' || r == ';' || r == '—' || isColon {
			c := strings.TrimSpace(part[start : i+size])
			switch {
			case c == "":
				start = i + size
			case !HasWordChar(c):
				// клиуза без букв (одна граница, «—»): в TTS не идёт,
				// граница остаётся в начале следующего фрагмента
				start = i
			default:
				out = append(out, c)
				start = i + size
			}
		}
	}
	return out, part[start:]
}

// HasWordChar — есть ли в s хотя бы одна буква/цифра (кирл/лат/цифры).
func HasWordChar(s string) bool {
	for _, r := range s {
		if (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') || (r >= '0' && r <= '9') ||
			(r >= 0x0400 && r <= 0x04FF) {
			return true
		}
	}
	return false
}

// TTSGap — число тише-кадров (250 мс) перед частью TTS по завершайющему знаку
// ПРЕДЫДУЮЩЕЙ части: «?/!» → 2 (500 мс), «.»/«...» → 1 (250 мс),
// «,»/«;»/«:»/«—» → 0 (клиузы одной фразы играют подряд).
func TTSGap(prev string) int {
	if prev == "" {
		return 0
	}
	switch {
	case strings.HasSuffix(prev, "?"), strings.HasSuffix(prev, "!"):
		return 2
	case strings.HasSuffix(prev, "."):
		return 1
	case strings.HasSuffix(prev, ","), strings.HasSuffix(prev, ";"),
		strings.HasSuffix(prev, ":"), strings.HasSuffix(prev, "—"):
		return 0
	}
	return 1 // без завершителя (хвост оборванного стрима) — консервативно
}

// IsUpperNext — следующий после индекса i символ — заглавная (лат/кирл).
func IsUpperNext(text string, i int) bool {
	if i >= len(text) {
		return false
	}
	r, size := utf8.DecodeRuneInString(text[i:])
	if size <= 1 {
		return false // уже обработано ASCII-путём
	}
	return (r >= 'A' && r <= 'Z') || (r >= 0x0410 && r <= 0x042F)
}

// TruncateForLog — обрезает s до n символов (по runem), добавляя «…» при обрезке.
func TruncateForLog(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n]) + "…"
}

// latinWord — латинское слово (для translit/терминов).
var latinWord = regexp.MustCompile(`[A-Za-z][A-Za-z+#./]*`)

// termTranslit — частые термины (в нижнем регистре, по слову).
var termTranslit = map[string]string{
	"go": "гоу", "python": "питон", "javascript": "джава скрипт", "typescript": "тайп скрипт",
	"java": "джава", "c#": "шарп", "c++": "си плюс", "rust": "раст", "php": "пи эч пи",
	"kotlin": "котлин", "swift": "свифт", "ruby": "руби", "scala": "скала",
	"docker": "докер", "kubernetes": "кубернетес", "k8s": "кубес", "redis": "редис",
	"postgres": "постгрес", "postgresql": "постгрес скл", "mysql": "май скл", "sqlite": "сайт скл",
	"mongodb": "монго дб", "kafka": "кафка", "rabbitmq": "рабит мкью", "git": "гит",
	"github": "гит хаб", "gitlab": "гит лаб", "api": "эй пи ай", "rest": "рест", "grpc": "джи ар пи си",
	"microservice": "микросервис", "microservices": "микросервисы", "middleware": "мидлвар",
	"backend": "бэкэнд", "frontend": "фронтэнд", "framework": "фреймворк", "database": "датабейз",
	"deployment": "диплоймент", "scaling": "скейлинг", "load": "лоад", "highload": "хайлоад",
	"benchmark": "бенчмарк", "profiling": "профилинг", "commit": "коммит", "push": "пуш",
}

// charTranslit — по букве (фолбэк для неизвестных слов).
var charTranslit = map[rune]string{
	'a': "а", 'b': "б", 'c': "с", 'd': "д", 'e': "э", 'f': "ф", 'g': "г", 'h': "х",
	'i': "и", 'j': "дж", 'k': "к", 'l': "л", 'm': "м", 'n': "н", 'o': "о", 'p': "п",
	'q': "к", 'r': "р", 's': "с", 't': "т", 'u': "у", 'v': "в", 'w': "в", 'x': "кс",
	'y': "й", 'z': "з",
}

// TranslitWord — слово по буквам (фолбэк), сохраняет капс первой буквы.
func TranslitWord(w string) string {
	if w == "" {
		return w
	}
	var b strings.Builder
	for i, r := range w {
		m, ok := charTranslit[lower(r)]
		if !ok {
			b.WriteRune(r)
			continue
		}
		if i == 0 && r >= 'A' && r <= 'Z' {
			m = strings.ToUpper(m)
		}
		b.WriteString(m)
	}
	return b.String()
}

func lower(r rune) rune {
	if r >= 'A' && r <= 'Z' {
		return r + ('a' - 'A')
	}
	return r
}

// PrepareTTS — текст для синтеза: латинские термины -> русское написание.
// Пунктуация, цифры и кириллица проходят без изменений.
func PrepareTTS(text string) string {
	return latinWord.ReplaceAllStringFunc(text, func(w string) string {
		key := strings.ToLower(w)
		if t, ok := termTranslit[key]; ok {
			return t
		}
		return TranslitWord(w)
	})
}
