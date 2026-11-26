// fsutil — файловая часть песочницы: рабочий каталог прогона, запись
// файлов кандидата/градатора и go-оверлей (вынесено из runner.go).
package runner

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// graderToken — плейсхолдер каталога доверенных файлов в команде прогона.
const graderToken = "%GRADER%"

// writeTree — пишет файлы в каталог (пути относительные, подкаталоги
// создаются) с указанными правами.
func writeTree(dir string, files map[string]string, perm os.FileMode) error {
	for path, content := range files {
		full := filepath.Join(dir, filepath.FromSlash(path))
		if err := os.MkdirAll(filepath.Dir(full), 0o700); err != nil {
			return fmt.Errorf("mkdir %s: %w", path, err)
		}
		if err := os.WriteFile(full, []byte(content), perm); err != nil {
			return fmt.Errorf("write %s: %w", path, err)
		}
	}
	return nil
}

// writeGoOverlay — go-оверлей: тесты банка подмешиваются в пакет кандидата на
// время сборки (`go test -overlay`), поэтому в рабочем каталоге решения их
// файлов нет вообще — ни solution_test.go на диске, ни чтения из init()/теста
// по относительному пути. Пустой Replace — чтобы команда не падала на задаче
// без тестов банка.
//
// Оверлеятся только *_test.go (то, что читает go-инструмент на сборке).
// shortcut: доверенные go-файлы другого вида (например testdata/) банку не
// нужны; появятся — их надо отдавать отдельным ro-путём.
func writeGoOverlay(gradeDir, gradeRef, candRef, stack string, trusted map[string]string) error {
	if stack != "go" {
		return nil
	}
	replace := map[string]string{}
	for name := range trusted {
		if strings.HasSuffix(name, "_test.go") {
			replace[candRef+"/"+name] = gradeRef + "/" + name
		}
	}
	body, err := json.Marshal(map[string]any{"Replace": replace})
	if err != nil {
		return fmt.Errorf("overlay: %w", err)
	}
	return os.WriteFile(filepath.Join(gradeDir, "overlay.json"), body, 0o444)
}

// newWorkDir — рабочий каталог запуска.
// ВАЖНО: не в системном temp-root (/tmp) — Go игнорирует go.mod в /tmp
// ("ignoring go.mod in system temp root"), и `go test` не может стартовать.
func (r *Runner) newWorkDir() (string, error) {
	base := r.cfg.WorkDirBase
	if base == "" {
		base = os.Getenv("SANDBOX_WORKDIR_BASE")
	}
	if base == "" {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", fmt.Errorf("home dir: %w", err)
		}
		base = filepath.Join(home, ".local", "share", "grade-sandbox")
	}
	if err := os.MkdirAll(base, 0o700); err != nil {
		return "", fmt.Errorf("workdir base: %w", err)
	}
	// spec-14/C5: janitor — run-* каталоги копятся, если процесс убит
	// (SIGKILL) до defer RemoveAll. Чистим старше 24 ч по mtime.
	if entries, err := os.ReadDir(base); err == nil {
		stale := time.Now().Add(-24 * time.Hour)
		for _, e := range entries {
			if !e.IsDir() || !strings.HasPrefix(e.Name(), "run-") {
				continue
			}
			if info, err := e.Info(); err == nil && info.ModTime().Before(stale) {
				_ = os.RemoveAll(filepath.Join(base, e.Name()))
			}
		}
	}
	return os.MkdirTemp(base, "run-*")
}

// safePath — путь относительно, без обхода вверх.
func safePath(p string) bool {
	if p == "" || filepath.IsAbs(p) {
		return false
	}
	clean := filepath.Clean(filepath.FromSlash(p))
	if clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) {
		return false
	}
	return true
}
