package httpapi

import (
	"os"
	"path/filepath"
	"testing"
)

// Пути config-файлов уже дважды ломали прод (roles.json в контейнере,
// openings/tts-voices вне образа), поэтому порядок кандидатов — под тестом.
// Проверяем список и выбор: сам chdir+CWD в golang.org/issue-подтексте
// резолвит относительные пути нестабильно, не воспроизводим его здесь.
func TestConfigCandidates(t *testing.T) {
	got := configCandidates("config/tts-voices.json")
	want := []string{
		"config/tts-voices.json",              // запуск из services/api (make run-api)
		"services/api/config/tts-voices.json", // запуск из корня репо (run-all.sh)
		"../config/tts-voices.json",           // общий config/ деплоя (он же /app/config в контейнере)
	}
	if len(got) != len(want) {
		t.Fatalf("кандидатов %d, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("кандидат [%d] = %q, want %q (весь список %v)", i, got[i], want[i], got)
		}
	}
}

func TestFirstExistingConfig(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "config", "roles.json")
	b := filepath.Join(root, "services", "api", "config", "roles.json")
	write(t, b)

	// a нет → берётся существующий b
	if got := firstExistingConfig([]string{a, b}); got != b {
		t.Fatalf("без первого файла ждём %q, получили %q", b, got)
	}
	// первый существующий выигрывает
	write(t, a)
	if got := firstExistingConfig([]string{a, b}); got != a {
		t.Fatalf("при обоих файлах ждём %q, получили %q", a, got)
	}
	// каталог вместо файла не считается
	dir := filepath.Join(root, "config")
	if got := firstExistingConfig([]string{dir}); got != "" {
		t.Fatalf("каталог не кандидат, получили %q", got)
	}
	// нет ничего — пустая строка (вызывающий решит, что писать в базовый путь)
	if got := firstExistingConfig([]string{filepath.Join(root, "нет.json")}); got != "" {
		t.Fatalf("без файлов ждём \"\", получили %q", got)
	}
	if _, err := os.Stat(root); err != nil {
		t.Fatal(err)
	}
}

func write(t *testing.T, p string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
}
