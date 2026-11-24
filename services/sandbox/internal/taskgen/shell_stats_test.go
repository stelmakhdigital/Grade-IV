package taskgen

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// draftTask — содержимое черновика как задача банка (id по seed/грейду).
func draftTask(id string, d *Draft) *tasks.Task {
	return &tasks.Task{
		ID:        id,
		Stack:     "shell",
		Grades:    d.Grades,
		Title:     d.Title,
		Statement: d.Statement,
		Files:     d.Files,
	}
}

// TestShellStatsDeterministic — один seed даёт то же содержимое: без этого
// «воспроизвести задачу через месяц» невозможно.
func TestShellStatsDeterministic(t *testing.T) {
	for _, grade := range []string{"junior", "middle", "senior", "staff"} {
		a, err := Build("shell-stats", 4242, grade)
		if err != nil {
			t.Fatalf("build %s: %v", grade, err)
		}
		b, err := Build("shell-stats", 4242, grade)
		if err != nil {
			t.Fatalf("build %s (повтор): %v", grade, err)
		}
		ha, hb := tasks.Hash(a.Files), tasks.Hash(b.Files)
		if ha != hb {
			t.Fatalf("%s: seed 4242 дал разное содержимое (%s != %s)", grade, ha, hb)
		}
		if len(a.Mutants) < 2 {
			t.Fatalf("%s: мутантов %d, ожидалось >= 2", grade, len(a.Mutants))
		}
		for _, m := range a.Mutants {
			if m["solution.sh"] == a.Reference["solution.sh"] {
				t.Fatalf("%s: мутант совпадает с эталоном — правка не сработала", grade)
			}
		}
	}
}

// TestShellStatsDataGuarantees — данные содержат то, на чём мутанты обязаны
// ловиться. Проверяется по сгенерированному input.txt, а не по внутренностям.
func TestShellStatsDataGuarantees(t *testing.T) {
	for _, tc := range []struct {
		grade   string
		needNeg bool
	}{
		{grade: "junior"},
		{grade: "middle", needNeg: true},
		{grade: "senior", needNeg: true},
		{grade: "staff", needNeg: true},
	} {
		for _, seed := range []int64{1, 7, 99, 12345} {
			d, err := Build("shell-stats", seed, tc.grade)
			if err != nil {
				t.Fatalf("build %s/%d: %v", tc.grade, seed, err)
			}
			in := d.Files["input.txt"]
			if !strings.Contains(in, "NA") {
				t.Fatalf("%s/%d: в данных нет мусорного NA — мутант «NA как 0» был бы тихим:\n%s", tc.grade, seed, in)
			}
			level := gradeLevel(tc.grade)
			nums := statsExpected(mustParse(t, in), level)
			if _, ok := nums["median"]; level >= 3 && !ok {
				t.Fatalf("%s/%d: медиана не вычисляется", tc.grade, seed)
			}
			if tc.needNeg && !strings.Contains(in, "-") {
				t.Fatalf("%s/%d: в данных нет отрицательных чисел:\n%s", tc.grade, seed, in)
			}
			if level >= 4 && nums["mode"] == "" {
				t.Fatalf("%s/%d: mode пустой", tc.grade, seed)
			}
		}
	}
}

func mustParse(t *testing.T, in string) []int64 {
	t.Helper()
	var out []int64
	for _, tok := range strings.FieldsFunc(in, func(r rune) bool { return r == ' ' || r == '\t' || r == ',' || r == '\n' }) {
		if tok == "NA" {
			continue
		}
		var v int64
		if _, err := fmt.Sscanf(tok, "%d", &v); err != nil {
			continue
		}
		out = append(out, v)
	}
	if len(out) == 0 {
		t.Fatalf("в input.txt не разобрано ни одного числа")
	}
	return out
}

// TestShellStatsGate — главное: эталон проходит, мутанты валятся, на всех
// грейдах и на нескольких seed. Это те же прогоны, что делает gate в сервисе.
func TestShellStatsGate(t *testing.T) {
	if testing.Short() {
		t.Skip("реальные прогоны bash в песочнице (не -short)")
	}
	rn := runner.New(runner.Config{Mode: "subprocess", Timeout: 30 * time.Second})
	for _, grade := range []string{"junior", "middle", "senior", "staff"} {
		for _, seed := range []int64{1, 7, 99} {
			d, err := Build("shell-stats", seed, grade)
			if err != nil {
				t.Fatalf("build %s/%d: %v", grade, seed, err)
			}
			task := draftTask(fmt.Sprintf("shell-stats-%d-%s", seed, grade), d)
			v, err := Check(context.Background(), rn, Request{
				Task:      task,
				Reference: d.Reference,
				Mutants:   d.Mutants,
			})
			if err != nil {
				t.Fatalf("gate %s/%d: %v", grade, seed, err)
			}
			if !v.OK {
				ev, _ := json.Marshal(v)
				t.Errorf("gate отбраковал %s (seed %d): %s", grade, seed, ev)
			}
		}
	}
}
