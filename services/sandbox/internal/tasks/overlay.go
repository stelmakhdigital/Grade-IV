package tasks

// spec-24: банк перестаёт быть только встроенным. Сгенерированные (и прогнавшие
// pre-flight gate) задачи дописываются в файл поверх embed-банка и переживают
// рестарт. Файл — единственный источник правды для этих задач, включая файлы
// градатора: наружу они не отдаются (см. IsGraderFile и trusted-файлы runner'а).

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

// Ограничения на принимаемую задачу (spec-24): содержимое приходит по HTTP из
// сервиса генерации, поэтому размеры и имена проверяются, а не «на доверии».
const (
	MaxFiles       = 12
	MaxFileBytes   = 128 << 10
	MaxTotalBytes  = 512 << 10
	MaxStatement   = 8000
	maxTitleLength = 200
)

var taskIDPattern = regexp.MustCompile(`^[a-z][a-z0-9]([a-z0-9-]{1,62})[a-z0-9]$`)

var knownStacks = map[string]bool{"go": true, "python": true, "shell": true}
var knownGrades = map[string]bool{"junior": true, "middle": true, "senior": true, "staff": true}

// ErrDuplicateID — id уже занят (в т.ч. встроенной задачей банка).
var ErrDuplicateID = errors.New("задача с таким id уже есть в банке")

// Load — embed-банк + overlay из file (если файл есть и он указан).
func Load(file string) (*Bank, error) {
	// Свежая копия встроенных задач, не singleton из Default(): банк с overlay
	// умеет добавлять задачи, и эти добавления не должны протекать в общее
	// встроенное состояние (и обратно — в чужой банк).
	list, byID, err := parseEmbedded()
	if err != nil {
		return nil, err
	}
	bank := &Bank{tasks: list, byID: byID, file: file}
	bank.mu.Lock()
	defer bank.mu.Unlock()
	if file == "" {
		return bank, nil
	}
	raw, err := os.ReadFile(file)
	if errors.Is(err, os.ErrNotExist) {
		return bank, nil
	}
	if err != nil {
		return nil, fmt.Errorf("чтение банка генераций %s: %w", file, err)
	}
	if len(strings.TrimSpace(string(raw))) == 0 {
		return bank, nil
	}
	var gen []*Task
	if err := json.Unmarshal(raw, &gen); err != nil {
		return nil, fmt.Errorf("разбор %s: %w", file, err)
	}
	for _, t := range gen {
		if err := validate(t); err != nil {
			return nil, fmt.Errorf("%s: %w", file, err)
		}
		// Overlay — только сгенерированные задачи: без meta запись не отличить
		// от встроенной, и flush потерял бы её при первой перезаписи файла.
		if t.Meta == nil || t.Meta.Family == "" {
			return nil, fmt.Errorf("%s: задача %s в overlay без meta", file, t.ID)
		}
		if _, dup := bank.byID[t.ID]; dup {
			// Сгенерированная задача не имеет права затирать встроенную: id
			// встроеного банка закреплён в событиях прошлых сессий.
			return nil, fmt.Errorf("%s: id %s конфликтует со встроенным банком", file, t.ID)
		}
		bank.byID[t.ID] = t
		bank.tasks = append(bank.tasks, t)
	}
	return bank, nil
}

// File — путь overlay-файла (пусто — банк read-only).
func (b *Bank) File() string {
	b.mu.RLock()
	defer b.mu.RUnlock()
	return b.file
}

// GeneratedCount — сколько задач пришло из overlay-файла (для /healthz).
func (b *Bank) GeneratedCount() int {
	b.mu.RLock()
	defer b.mu.RUnlock()
	n := 0
	for _, t := range b.tasks {
		if t.Meta != nil {
			n++
		}
	}
	return n
}

// Add — добавить сгенерированную задачу и сохранить overlay (если путь задан).
func (b *Bank) Add(t *Task) error {
	if err := validate(t); err != nil {
		return err
	}
	if t.Meta == nil {
		return errors.New("у сгенерированной задачи должен быть meta")
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.file == "" {
		return errors.New("банк открыт без overlay-файла: добавление задач невозможно")
	}
	if _, dup := b.byID[t.ID]; dup {
		return ErrDuplicateID
	}
	if t.Meta.Hash == "" {
		t.Meta.Hash = Hash(t.Files)
	}
	b.byID[t.ID] = t
	b.tasks = append(b.tasks, t)
	return b.flushLocked()
}

// flushLocked — атомарная запись overlay (tmp + rename). Вызывать с b.mu.
func (b *Bank) flushLocked() error {
	list := make([]*Task, 0, len(b.tasks))
	for _, t := range b.tasks {
		if t.Meta != nil {
			list = append(list, t)
		}
	}
	sort.Slice(list, func(i, j int) bool { return list[i].ID < list[j].ID })
	raw, err := json.MarshalIndent(list, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(b.file), 0o755); err != nil {
		return err
	}
	tmp := b.file + ".tmp"
	// 0600: файлы градатора не должны читаться посторонними процессами хоста.
	if err := os.WriteFile(tmp, append(raw, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, b.file)
}

// validate — проверка принимаемой задачи (размеры, имена, состав файлов).
func validate(t *Task) error {
	if t == nil {
		return errors.New("пустая задача")
	}
	if !taskIDPattern.MatchString(t.ID) {
		return fmt.Errorf("недопустимый id %q (строчные латинские, цифры и дефис, 3..64)", t.ID)
	}
	if !knownStacks[t.Stack] {
		return fmt.Errorf("неизвестный стек %q", t.Stack)
	}
	if len(t.Grades) == 0 {
		return errors.New("не указан грейд задачи")
	}
	for _, g := range t.Grades {
		if !knownGrades[g] {
			return fmt.Errorf("неизвестный грейд %q", g)
		}
	}
	if strings.TrimSpace(t.Title) == "" || len(t.Title) > maxTitleLength {
		return fmt.Errorf("заголовок пустой или длиннее %d", maxTitleLength)
	}
	if strings.TrimSpace(t.Statement) == "" || len(t.Statement) > MaxStatement {
		return fmt.Errorf("условие пустое или длиннее %d", MaxStatement)
	}
	if len(t.Files) == 0 || len(t.Files) > MaxFiles {
		return fmt.Errorf("файлов %d, допустимо 1..%d", len(t.Files), MaxFiles)
	}
	total, graded, cand := 0, 0, 0
	for name, content := range t.Files {
		if err := validateFileName(name); err != nil {
			return err
		}
		if len(content) > MaxFileBytes {
			return fmt.Errorf("файл %s больше %d байт", name, MaxFileBytes)
		}
		total += len(content)
		if IsGraderFile(name, t.Stack) {
			graded++
		} else {
			cand++
		}
	}
	if total > MaxTotalBytes {
		return fmt.Errorf("суммарно %d байт, предел %d", total, MaxTotalBytes)
	}
	// Без градатора задачу нечем проверить, без файла кандидата нечего сдавать.
	if graded == 0 {
		return fmt.Errorf("нет ни одного файла градатора (стек %s)", t.Stack)
	}
	if cand == 0 {
		return errors.New("нет ни одного файла кандидата")
	}
	return nil
}

func validateFileName(name string) error {
	if name == "" || len(name) > 64 {
		return fmt.Errorf("недопустимое имя файла %q", name)
	}
	if strings.HasPrefix(name, "/") || strings.Contains(name, "\\") {
		return fmt.Errorf("недопустимое имя файла %q", name)
	}
	clean := filepath.ToSlash(filepath.Clean(name))
	if clean != name || clean == "." || clean == ".." || strings.HasPrefix(clean, "../") {
		return fmt.Errorf("недопустимое имя файла %q", name)
	}
	return nil
}

// Hash — стабильный хэш содержимого задачи (sorted name\x00content\x00).
func Hash(files map[string]string) string {
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	sort.Strings(names)
	h := sha256.New()
	for _, n := range names {
		h.Write([]byte(n))
		h.Write([]byte{0})
		h.Write([]byte(files[n]))
		h.Write([]byte{0})
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}
