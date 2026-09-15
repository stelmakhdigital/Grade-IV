package httpapi

import (
	"regexp"
	"strings"
)

// Подготовка текста для TTS (Silero v5 — русская модель):
// латиница в тексте (названия технологий) озвучивается как «проглочена»
// («какие проекты делал на Go» -> «...делал на»). Для синтеза заменяем
// латинские термины на русское написание; текст кандидату в UI остаётся
// оригинальным (prepareTTS применяется только к потоку TTS).

// termTranslit — частые термины (в нижнем регистре, по слову).
var termTranslit = map[string]string{
	"go": "гоу",
	"python": "питон",
	"javascript": "джава скрипт",
	"typescript": "тайп скрипт",
	"java": "джава",
	"c#": "шарп",
	"c++": "си плюс",
	"rust": "раст",
	"php": "пи эч пи",
	"kotlin": "котлин",
	"swift": "свифт",
	"ruby": "руби",
	"scala": "скала",
	"docker": "докер",
	"kubernetes": "кубернетес",
	"k8s": "кубес",
	"redis": "редис",
	"postgres": "постгрес",
	"postgresql": "постгрес скл",
	"mysql": "май скл",
	"sqlite": "сайт скл",
	"mongodb": "монго дб",
	"kafka": "кафка",
	"rabbitmq": "рабит мкью",
	"git": "гит",
	"github": "гит хаб",
	"gitlab": "гит лаб",
	"api": "эй пи ай",
	"rest": "рест",
	"grpc": "джи ар пи си",
	"microservice": "микросервис",
	"microservices": "микросервисы",
	"middleware": "мидлвар",
	"backend": "бэкэнд",
	"frontend": "фронтэнд",
	"framework": "фреймворк",
	"database": "датабейз",
	"deployment": "диплоймент",
	"scaling": "скейлинг",
	"load": "лоад",
	"highload": "хайлоад",
	"benchmark": "бенчмарк",
	"profiling": "профилинг",
	"commit": "коммит",
	"push": "пуш",
	"pull": "пул",
	"merge": "мердж",
	"rebase": "рибейз",
	"branch": "бранч",
	"review": "ревью",
	"coding": "кодинг",
	"debug": "дебаг",
	"release": "релиз",
	"sprint": "спринт",
	"agile": "аджайл",
	"ci/cd": "си ай / си ди",
	"llm": "эл эл эм",
	"tts": "ти ти эс",
	"stt": "эс ти ти",
	"http": "этч пи ти",
	"https": "этч пи ти с",
	"html": "этч эм эл",
	"css": "си эс эс",
	"json": "джай сон",
	"yaml": "ямл",
	"xml": "экхс эс эм",
	"sql": "эс кью эл",
	"tcp": "ти ци пи",
	"udp": "у ди пи",
	"gpu": "джи пи ю",
	"cpu": "си пи ю",
	"ram": "рам",
	"ssd": "эс эс ди",
	"url": "ю эй эл",
	"ui": "ю и",
	"id": "ай ди",
}

// latinWord — слово из латиницы с допустимыми суффиксами «+»/«#»/«/»/«.»
// (c++, c#, ci/cd, i.e.). Дефис не входит: «Go-сервис» -> «Go» отдельно.
var latinWord = regexp.MustCompile(`[A-Za-z][A-Za-z+#./]*`)

// charTranslit — по букве (фолбэк для неизвестных слов).
var charTranslit = map[rune]string{
	'a': "а", 'b': "б", 'c': "с", 'd': "д", 'e': "э", 'f': "ф",
	'g': "г", 'h': "х", 'i': "и", 'j': "дж", 'k': "к", 'l': "л",
	'm': "м", 'n': "н", 'o': "о", 'p': "п", 'q': "к", 'r': "р",
	's': "с", 't': "т", 'u': "у", 'v': "в", 'w': "в", 'x': "кс",
	'y': "й", 'z': "з",
}

// translitWord — fallback: латинское слово по буквам в кириллицу.
// Сохраняет «капс» первого символа, если исходное слово с заглавной.
func translitWord(w string) string {
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

// prepareTTS — текст для синтеза: латинские термины -> русское написание.
// Пунктуация, цифры и кириллица проходят без изменений.
func prepareTTS(text string) string {
	return latinWord.ReplaceAllStringFunc(text, func(w string) string {
		key := strings.ToLower(w)
		if t, ok := termTranslit[key]; ok {
			return t
		}
		return translitWord(w)
	})
}
