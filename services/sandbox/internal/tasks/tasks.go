// Package tasks — банк задач Live-Code (WP-6): Go и Python, теги по грейдам.
// Задачи встраиваются в бинарник (go:embed) — сеть для загрузок не нужна (ADR-003:
// зависимости фиксируются в банке).
package tasks

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"path"
	"path/filepath"
	"strings"
	"sync"
)

//go:embed tasks.json
var rawTasks string

// Task — задача банка.
type Task struct {
	ID        string            `json:"id"`
	Stack     string            `json:"stack"` // go | python | shell
	Grades    []string          `json:"grades"`
	Title     string            `json:"title"`
	Statement string            `json:"statement"`
	Files     map[string]string `json:"files"`
	// Meta — происхождение задачи (spec-24): у сгенерированных задач семья, seed
	// и хэш содержимого, чтобы отчёт можно было пересобрать и оспорить.
	Meta *Meta `json:"meta,omitempty"`
}

// Meta — происхождение задачи (spec-24).
type Meta struct {
	Family    string `json:"family"`
	Seed      int64  `json:"seed,omitempty"`
	Generator string `json:"generator,omitempty"`
	Hash      string `json:"hash"`
}

// Bank — банк задач: встроенный JSON плюс overlay из файла (spec-24).
type Bank struct {
	mu    sync.RWMutex
	tasks []*Task
	byID  map[string]*Task
	// file — куда писать overlay (пусто — банк только из embed, Add запрещён).
	file string
}

var (
	defaultBankOnce sync.Once
	defaultBank     *Bank
	defaultBankErr  error
)

// Default — глобальный банк из встраиваемого JSON.
func Default() (*Bank, error) {
	defaultBankOnce.Do(func() {
		list, byID, err := parseEmbedded()
		if err != nil {
			defaultBankErr = err
			return
		}
		defaultBank = &Bank{tasks: list, byID: byID}
	})
	return defaultBank, defaultBankErr
}

// parseEmbedded — свежие задачи из встраиваемого JSON. Каждый банк получает
// свою копию: банк, в который можно добавлять задачи (overlay), не должен
// менять общее встроенное состояние.
func parseEmbedded() ([]*Task, map[string]*Task, error) {
	var list []*Task
	if err := json.Unmarshal([]byte(rawTasks), &list); err != nil {
		return nil, nil, fmt.Errorf("разбор tasks.json: %w", err)
	}
	byID := make(map[string]*Task, len(list))
	for _, t := range list {
		if _, dup := byID[t.ID]; dup {
			return nil, nil, fmt.Errorf("дубликат задачи: %s", t.ID)
		}
		byID[t.ID] = t
	}
	return list, byID, nil
}

// IsTrustedFile — файлы, которые кандидат не задаёт своим сабмитом: тесты/чеки
// (IsGraderFile) и данные, на которых они работают. Отданные кандидотом версии
// таких файлов раннер выбрасывает (см. runner): иначе «пришли свой check.sh»
// превращается в «получи pass» (issue #8, issue #9).
func IsTrustedFile(name, stack string) bool {
	if IsGraderFile(name, stack) {
		return true
	}
	if stack != "shell" {
		return false
	}
	switch filepath.Base(name) {
	case "input.txt", "empty.txt", "data.txt":
		return true
	}
	return false
}

// Get — задача по ID.
func (b *Bank) Get(id string) (*Task, bool) {
	b.mu.RLock()
	defer b.mu.RUnlock()
	t, ok := b.byID[id]
	return t, ok
}

// List — задачи по стеку и/или грейду (пустые параметры — без фильтра).
func (b *Bank) List(stack, grade string) []*Task {
	b.mu.RLock()
	defer b.mu.RUnlock()
	out := make([]*Task, 0, len(b.tasks))
	for _, t := range b.tasks {
		if stack != "" && t.Stack != stack {
			continue
		}
		if grade != "" {
			matched := false
			for _, g := range t.Grades {
				if g == grade {
					matched = true
					break
				}
			}
			if !matched {
				continue
			}
		}
		out = append(out, t)
	}
	return out
}

// Count — общее число задач.
func (b *Bank) Count() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return len(b.tasks)
}

// IsGraderFile — файл градатора задачи (доверенный): его нельзя отдавать
// кандидату ни в редактор, ни в рабочий каталог прогона (BUG-20261104-027 —
// граница WS, BUG-20261122-033 — граница песочницы).
// go: *_test.go, python: test_*.py / *_test.py, shell: check.sh.
func IsGraderFile(name, stack string) bool {
	switch stack {
	case "go":
		return strings.HasSuffix(name, "_test.go")
	case "python":
		base := path.Base(name)
		return strings.HasPrefix(base, "test_") || strings.HasSuffix(base, "_test.py")
	case "shell":
		// spec-19: чек-скрипт банка — доверенный (кандидат правит только
		// solution.sh; подмена check.sh из запроса игнорируется).
		return name == "check.sh"
	default:
		return false
	}
}
