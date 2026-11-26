package httpapi

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Сохранение из админки не должно стирать служебные ключи (_comment/_README),
// которые оператор дописал в конфиге руками: marshal по структуре их теряет.
func TestWithMetaKeysKeepsComments(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "tts-voices.json")
	old := `{"_comment":"правьте руками","_README":"спикеры: aidar, xenia","default":"xenia","voices":[{"id":"xenia","speaker":"xenia"}]}`
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	// То, что собрал handler: структуры без мета-ключей.
	payload, err := json.MarshalIndent(map[string]any{
		"default": "aidar",
		"voices":  []any{map[string]any{"id": "aidar", "speaker": "aidar"}},
	}, "", "  ")
	if err != nil {
		t.Fatal(err)
	}

	got := withMetaKeys(path, payload)

	var merged map[string]json.RawMessage
	if err := json.Unmarshal(got, &merged); err != nil {
		t.Fatalf("результат не JSON: %v (%s)", err, got)
	}
	for _, k := range []string{"_comment", "_README"} {
		if _, ok := merged[k]; !ok {
			t.Errorf("ключ %s потерян: %s", k, got)
		}
	}
	if string(merged["default"]) != `"aidar"` {
		t.Errorf("данные перезаписи повреждены: %s", merged["default"])
	}
	// Мета-ключ идёт первым (в файлах он стоит сверху) — json.Marshal сортирует
	// ключи, "_" меньше букв.
	if strings.Index(string(got), `"_comment"`) > strings.Index(string(got), `"default"`) {
		t.Error("_comment должен стоять перед данными")
	}
}

// Файла нет / он битый / мета-ключей нет — payload проходит нетронутым:
// потеря комментариев не повод отказывать в сохранении.
func TestWithMetaKeysPassthrough(t *testing.T) {
	dir := t.TempDir()
	payload := []byte(`{"default":"xenia"}`)

	if got := withMetaKeys(filepath.Join(dir, "нет-такого.json"), payload); string(got) != string(payload) {
		t.Errorf("нет файла: %s", got)
	}
	broken := filepath.Join(dir, "openings.json")
	if err := os.WriteFile(broken, []byte("{это не json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := withMetaKeys(broken, payload); string(got) != string(payload) {
		t.Errorf("битый файл: %s", got)
	}
	noMeta := filepath.Join(dir, "roles.json")
	if err := os.WriteFile(noMeta, []byte(`{"default":"aidar"}`), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := withMetaKeys(noMeta, payload); string(got) != string(payload) {
		t.Errorf("файл без мета-ключей: %s", got)
	}
}
