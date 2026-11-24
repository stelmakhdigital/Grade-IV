package tasks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func genTask(id string) *Task {
	return &Task{
		ID:        id,
		Stack:     "shell",
		Grades:    []string{"junior"},
		Title:     "Тестовая задача",
		Statement: "Напечатать hello.",
		Files: map[string]string{
			"solution.sh": "#!/usr/bin/env bash\n",
			"check.sh":    "#!/usr/bin/env bash\necho OK\n",
		},
		Meta: &Meta{Family: "test", Seed: 1},
	}
}

// TestLoadFileAbsent — файла нет это «генерации ещё не было», не ошибка.
func TestLoadFileAbsent(t *testing.T) {
	b, err := Load(filepath.Join(t.TempDir(), "nope.json"))
	if err != nil {
		t.Fatalf("Load несуществующего overlay: %v", err)
	}
	if b.GeneratedCount() != 0 {
		t.Fatalf("GeneratedCount = %d, ожидалось 0", b.GeneratedCount())
	}
	if _, ok := b.Get("go-reverse-string"); !ok {
		t.Fatalf("встроенный банк не загрузился")
	}
}

// TestLoadRejectsBrokenOverlay — битая задача в файле пула не должна тихо
// попадать в банк: лучше отказ на старте, чем непроверяемая задача у кандидата.
func TestLoadRejectsBrokenOverlay(t *testing.T) {
	cases := map[string]func(*Task){
		"нет файлов градатора": func(tl *Task) { delete(tl.Files, "check.sh") },
		"id не по шаблону":     func(tl *Task) { tl.ID = "Bad ID!" },
		"нет grades":           func(tl *Task) { tl.Grades = nil },
		"неизвестный стек":     func(tl *Task) { tl.Stack = "ruby" },
		"нет meta":             func(tl *Task) { tl.Meta = nil },
	}
	for name, breakIt := range cases {
		t.Run(name, func(t *testing.T) {
			tl := genTask("gen-ok-1")
			breakIt(tl)
			file := filepath.Join(t.TempDir(), "gen.json")
			writeOverlay(t, file, []*Task{tl})
			if _, err := Load(file); err == nil {
				t.Fatalf("Load принял битую задачу (%s)", name)
			}
		})
	}
}

// TestAddWithoutFile — банк, открытый без overlay, read-only.
func TestAddWithoutFile(t *testing.T) {
	b, err := Load("")
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Add(genTask("gen-x-1")); err == nil {
		t.Fatalf("Add в банк без overlay-файла прошёл")
	}
}

// TestAddRejectsEmbeddedID — сгенерированная задача не затирает встроенную:
// id встроенных задач закреплены в событиях прошлых сессий.
func TestAddRejectsEmbeddedID(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gen.json")
	b, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	tl := genTask("go-reverse-string")
	if err := b.Add(tl); err != ErrDuplicateID {
		t.Fatalf("Add с встроенным id вернул %v, ожидалось ErrDuplicateID", err)
	}
	got, ok := b.Get("go-reverse-string")
	if !ok || got.Title == tl.Title {
		t.Fatalf("встроенная задача изменена: %+v", got)
	}
}

// TestAddPersistsAndIsolated — принятая задача переживает перезагрузку и не
// утекает в singleton Default() (общее встроенное состояние менять нельзя).
func TestAddPersistsAndIsolated(t *testing.T) {
	file := filepath.Join(t.TempDir(), "gen.json")
	b, err := Load(file)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if err := b.Add(genTask("gen-keep-1")); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if b.GeneratedCount() != 1 {
		t.Fatalf("GeneratedCount = %d, ожидалось 1", b.GeneratedCount())
	}

	b2, err := Load(file)
	if err != nil {
		t.Fatalf("повторный Load: %v", err)
	}
	if _, ok := b2.Get("gen-keep-1"); !ok {
		t.Fatalf("задача пропала после перезагрузки")
	}
	if b2.GeneratedCount() != 1 {
		t.Fatalf("после перезагрузки GeneratedCount = %d", b2.GeneratedCount())
	}

	embed, err := Default()
	if err != nil {
		t.Fatalf("Default: %v", err)
	}
	if _, ok := embed.Get("gen-keep-1"); ok {
		t.Fatalf("сгенерированная задача утекла во встроенный банк (singleton)")
	}
	// И overlay не должен перезаписать встроенную задачу при загрузке.
	dup := genTask("go-two-sum")
	file2 := filepath.Join(t.TempDir(), "dup.json")
	writeOverlay(t, file2, []*Task{dup})
	if _, err := Load(file2); err == nil {
		t.Fatalf("Load пустил overlay-задачу с встроенным id")
	}
}

// TestHashStable — meta.hash обязан быть одинаковым при одинаковом содержимом
// (map в Go обходится в случайном порядке) и разным при разном.
func TestHashStable(t *testing.T) {
	files := map[string]string{
		"check.sh":    "#!/usr/bin/env bash\necho OK\n",
		"solution.sh": "#!/usr/bin/env bash\n",
		"input.txt":   "1 2 3\n",
	}
	first := Hash(files)
	if first == "" {
		t.Fatalf("Hash вернул пустую строку")
	}
	for i := 0; i < 20; i++ {
		if got := Hash(map[string]string{
			"input.txt":   "1 2 3\n",
			"check.sh":    "#!/usr/bin/env bash\necho OK\n",
			"solution.sh": "#!/usr/bin/env bash\n",
		}); got != first {
			t.Fatalf("Hash поехал от порядка ключей: %s != %s", got, first)
		}
	}
	files2 := map[string]string{"a": "1"}
	if Hash(files2) == Hash(map[string]string{"a": "2"}) {
		t.Fatalf("Hash различает содержимое")
	}
}

// TestIsTrustedFile — данные, на которых работает чек, кандидат задать не может.
func TestIsTrustedFile(t *testing.T) {
	yes := [][2]string{{"check.sh", "shell"}, {"input.txt", "shell"}, {"empty.txt", "shell"}, {"grade_test.go", "go"}, {"test_a.py", "python"}}
	for _, c := range yes {
		if !IsTrustedFile(c[0], c[1]) {
			t.Errorf("%s (%s) должен быть доверенным", c[0], c[1])
		}
	}
	no := [][2]string{{"solution.sh", "shell"}, {"main.go", "go"}, {"a.py", "python"}, {"input.txt", "go"}}
	for _, c := range no {
		if IsTrustedFile(c[0], c[1]) {
			t.Errorf("%s (%s) — файл кандидата, не доверенный", c[0], c[1])
		}
	}
}

func writeOverlay(t *testing.T, file string, list []*Task) {
	t.Helper()
	raw, err := json.Marshal(list)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if err := os.WriteFile(file, raw, 0o600); err != nil {
		t.Fatalf("write overlay: %v", err)
	}
}
