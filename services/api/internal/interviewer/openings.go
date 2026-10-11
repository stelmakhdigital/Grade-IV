package interviewer

import (
	"encoding/json"
	"math/rand"
	"os"
	"sync"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// F2 (Итерация 10): настраиваемые СЛУЧАЙНЫЕ начала собеседования (по грейду).
// Хранятся как ДАННЫЕ (JSON-файл в репо), не хардкод в Go. При старте голосовой
// стадии выбирается случайный подходящий (по грейду; '*' — для любого) вариант,
// избегая повторения последнего для пользователя. Путь к файлу — env/конфиг
// (INTRO_OPENINGS_FILE), дефолт — файл в репо (config/openings.json).

// Opening — одна запись набора начал.
type Opening struct {
	// Grade — грейд: junior|middle|senior|staff или '*' (любой).
	Grade string `json:"grade"`
	// Prompt — инструкция/вопрос для старта (LLM адаптирует, не дословно).
	Prompt string `json:"prompt"`
}

// openingsFile — формат файла набора начал.
type openingsFile struct {
	Openings []Opening `json:"openings"`
}

// Openings — набор начал + состояние «последний вариант» (анти-повтор).
type Openings struct {
	list []Opening
	mu   sync.Mutex
	last map[int64]string // userID -> последний prompt (не повторять сразу)
	rnd  *rand.Rand
}

// LoadOpenings — читает файл набора начал (path). Пустой/ошибочный файл —
// (nil, nil) с логированием? Нет: (nil, err). Ноль записей — (nil, nil) (вызов
// RandomFor вернёт "" и старт пойдёт по-старому, backward-compat).
func LoadOpenings(path string) (*Openings, error) {
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil // файла нет — дефолтное (старое) начало
		}
		return nil, err
	}
	var f openingsFile
	if err := json.Unmarshal(raw, &f); err != nil {
		return nil, err
	}
	if len(f.Openings) == 0 {
		return nil, nil
	}
	// Сид от времени (не фиксированный 0): «первый» выбор различается между
	// запусками api (истинная рандомность после рестарта). Конкурентный доступ
	// к rnd защищён o.mu (см. RandomFor).
	return &Openings{list: f.Openings, last: map[int64]string{}, rnd: rand.New(rand.NewSource(time.Now().UnixNano()))}, nil
}

// RandomFor — случайный вариант начала для грейда (учитывая userID для
// анти-повтора). Возвращает "" если подходящих нет (вызовчик идёт по-старому).
func (o *Openings) RandomFor(grade models.Grade, userID int64) string {
	if o == nil || len(o.list) == 0 {
		return ""
	}
	candidates := make([]Opening, 0, len(o.list))
	for _, op := range o.list {
		if op.Grade == string(grade) || op.Grade == "*" {
			if op.Prompt != "" {
				candidates = append(candidates, op)
			}
		}
	}
	if len(candidates) == 0 {
		return ""
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	last := o.last[userID]
	pool := candidates
	if len(candidates) > 1 {
		// не повторяем последний: отфильтруем (если есть альтернатива)
		alt := make([]Opening, 0, len(candidates))
		for _, c := range candidates {
			if c.Prompt != last {
				alt = append(alt, c)
			}
		}
		if len(alt) > 0 {
			pool = alt
		}
	}
	pick := pool[o.rnd.Intn(len(pool))]
	o.last[userID] = pick.Prompt
	return pick.Prompt
}
