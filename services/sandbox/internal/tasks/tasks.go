// Package tasks — банк задач Live-Code (WP-6): Go и Python, теги по грейдам.
// Задачи встраиваются в бинарник (go:embed) — сеть для загрузок не нужна (ADR-003:
// зависимости фиксируются в банке).
package tasks

import (
	_ "embed"
	"encoding/json"
	"fmt"
	"sync"
)

//go:embed tasks.json
var rawTasks string

// Task — задача банка.
type Task struct {
	ID        string            `json:"id"`
	Stack     string            `json:"stack"` // go | python
	Grades    []string          `json:"grades"`
	Title     string            `json:"title"`
	Statement string            `json:"statement"`
	Files     map[string]string `json:"files"`
}

// Bank — банк задач.
type Bank struct {
	mu    sync.RWMutex
	tasks []*Task
	byID  map[string]*Task
}

var (
	defaultBankOnce sync.Once
	defaultBank     *Bank
	defaultBankErr  error
)

// Default — глобальный банк из встраиваемого JSON.
func Default() (*Bank, error) {
	defaultBankOnce.Do(func() {
		var list []*Task
		if err := json.Unmarshal([]byte(rawTasks), &list); err != nil {
			defaultBankErr = fmt.Errorf("разбор tasks.json: %w", err)
			return
		}
		byID := make(map[string]*Task, len(list))
		for _, t := range list {
			if _, dup := byID[t.ID]; dup {
				defaultBankErr = fmt.Errorf("дубликат задачи: %s", t.ID)
				return
			}
			byID[t.ID] = t
		}
		defaultBank = &Bank{tasks: list, byID: byID}
	})
	return defaultBank, defaultBankErr
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
