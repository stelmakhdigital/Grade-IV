// Package taskgen — генерация задач Live-Code и pre-flight gate (spec-24).
//
// Смысл gate: задача попадает в банк только если её тесты действительно
// различают решения. Проверяются два факта: эталонное решение проходит, и каждое
// заведомо неправильное решение (мутант) падает. Без второго условия тесты
// «зелёные для всего» дали бы кандидату честный на вид, но пустой балл.
package taskgen

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/runner"
	"github.com/stelmakhdigital/grade-iv/services/sandbox/internal/tasks"
)

// MaxMutants — сколько мутантов прогоняем. Каждый мутант — полный прогон в
// песочнице: shell это десятки миллисекунд, go/python — холодная сборка, поэтому
// лимит есть, а не «сколько прислали».
const MaxMutants = 6

// Runner — то, что нужно gate от песочницы (реальный runner.Runner подходит;
// в тестах подменяется заглушкой).
type Runner interface {
	Run(ctx context.Context, stack string, files, trusted map[string]string) (runner.Result, error)
}

// Request — что проверяем: задача банка + эталон + мутанты. Файлы эталона и
// мутантов — файлы кандидата (файлы градатора берутся из задачи).
type Request struct {
	Task      *tasks.Task
	Reference map[string]string
	Mutants   []map[string]string
}

// Evidence — чем доказан вердикт (сниппет — чтобы отбраковку можно было прочитать).
type Evidence struct {
	Name     string `json:"name"`
	Passed   bool   `json:"passed"`
	Timeout  bool   `json:"timeout"`
	ExitCode int    `json:"exit_code"`
	Snippet  string `json:"snippet,omitempty"`
}

// Verdict — итог gate.
type Verdict struct {
	OK        bool       `json:"ok"`
	Reasons   []string   `json:"reasons,omitempty"`
	Reference *Evidence  `json:"reference,omitempty"`
	Mutants   []Evidence `json:"mutants,omitempty"`
}

// SplitFiles — делит содержимое задачи на файлы кандидата и файлы градатора
// (единственный источник правила — tasks.IsTrustedFile).
func SplitFiles(t *tasks.Task) (files, trusted map[string]string) {
	files, trusted = map[string]string{}, map[string]string{}
	for name, content := range t.Files {
		if tasks.IsTrustedFile(name, t.Stack) {
			trusted[name] = content
		} else {
			files[name] = content
		}
	}
	return files, trusted
}

// Check — прогон gate. Ошибки транспорта (песочница недоступна) возвращаются в
// error; непройденные проверки — в Verdict.OK=false с причинами.
func Check(ctx context.Context, rn Runner, req Request) (*Verdict, error) {
	if req.Task == nil {
		return nil, fmt.Errorf("gate: нет задачи")
	}
	if len(req.Reference) == 0 {
		return nil, fmt.Errorf("gate: нет эталонного решения для %s", req.Task.ID)
	}
	base, trusted := SplitFiles(req.Task)
	v := &Verdict{OK: true}

	ref, err := rn.Run(ctx, req.Task.Stack, mergeFiles(base, req.Reference), trusted)
	if err != nil {
		return nil, fmt.Errorf("gate: прогон эталона: %w", err)
	}
	v.Reference = evidence("reference", ref)
	switch {
	case ref.Timeout:
		v.OK = false
		v.Reasons = append(v.Reasons, "эталон не уложился в лимит — задача не будет решаемой за отведённое время")
	case !ref.Passed:
		v.OK = false
		v.Reasons = append(v.Reasons, "эталонное решение не прошло собственные тесты задачи")
	}

	// Мутанты ловятся все сразу (не с первым провалом): список причин полезнее
	// одной, и «тесты не различают» надо видеть целиком.
	for i, m := range req.Mutants {
		if i >= MaxMutants {
			v.Reasons = append(v.Reasons, fmt.Sprintf("проверен только %d мутантов из %d", MaxMutants, len(req.Mutants)))
			break
		}
		res, err := rn.Run(ctx, req.Task.Stack, mergeFiles(base, m), trusted)
		if err != nil {
			return nil, fmt.Errorf("gate: прогон мутанта %d: %w", i+1, err)
		}
		name := fmt.Sprintf("mutant-%d", i+1)
		v.Mutants = append(v.Mutants, *evidence(name, res))
		if res.Passed {
			v.OK = false
			v.Reasons = append(v.Reasons, fmt.Sprintf("%s прошёл тесты — тесты задачи не различают решения, балл по ней ничего не стоит", name))
		}
	}
	return v, nil
}

// mergeFiles — файлы задачи (заготовка кандидата) + сдача; сдача важнее.
func mergeFiles(base, submission map[string]string) map[string]string {
	out := make(map[string]string, len(base)+len(submission))
	for k, v := range base {
		out[k] = v
	}
	for k, v := range submission {
		out[k] = v
	}
	return out
}

func evidence(name string, r runner.Result) *Evidence {
	snip := strings.TrimSpace(r.Stdout + "\n" + r.Stderr)
	snip = strings.Join(strings.Fields(snip), " ")
	if len(snip) > 300 {
		snip = snip[:300]
	}
	return &Evidence{Name: name, Passed: r.Passed, Timeout: r.Timeout, ExitCode: r.ExitCode, Snippet: snip}
}

// SortedGrades — порядок грейдов для генерации (junior → staff).
func SortedGrades(grades []string) []string {
	order := map[string]int{"junior": 0, "middle": 1, "senior": 2, "staff": 3}
	out := append([]string(nil), grades...)
	sort.Slice(out, func(i, j int) bool { return order[out[i]] < order[out[j]] })
	return out
}
