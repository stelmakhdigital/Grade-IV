package httpapi

import (
	"testing"

	"github.com/stelmakhdigital/grade-iv/services/api/internal/models"
)

// cleanStacks — языки из ответа ИИ обязаны стать рабочим списком: порядок ИИ
// значим (первый — кодовая стадия), регистр и пробелы любые, неподдерживаемые
// значения («rust», «sql») и повторы выбрасываются.
func TestCleanStacks(t *testing.T) {
	got := cleanStacks([]models.Stack{" Python ", "python", "GO", "rust", "", "sql", "shell"})
	want := []models.Stack{models.StackPython, models.StackGo, models.StackShell}
	if len(got) != len(want) {
		t.Fatalf("cleanStacks = %v, хочу %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("cleanStacks = %v, хочу %v", got, want)
		}
	}

	if got := cleanStacks(nil); got != nil && len(got) != 0 {
		t.Errorf("пустой вход дал %v", got)
	}
}
