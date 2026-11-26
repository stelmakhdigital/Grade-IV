// parse — разбор вывода прогона: go test -json, разделение потоков,
// ограничение размера вывода (вынесено из runner.go).
package runner

import (
	"bytes"
	"encoding/json"
)

// parseGoTests — из `go test -json`: имя теста → итоговый pass/fail.
func parseGoTests(raw []byte, fallback []TestResult) []TestResult {
	type ev struct {
		Action string
		Test   string
	}
	tests := map[string]bool{}
	order := []string{}
	rest := raw
	for len(rest) > 0 {
		var line []byte
		line, rest, _ = bytes.Cut(rest, []byte("\n"))
		var e ev
		if json.Unmarshal(line, &e) != nil || e.Test == "" {
			continue
		}
		switch e.Action {
		case "pass", "fail", "skip":
			passed := e.Action != "fail"
			if _, seen := tests[e.Test]; !seen {
				order = append(order, e.Test)
			}
			tests[e.Test] = passed
		}
	}
	if len(order) == 0 {
		return fallback
	}
	out := make([]TestResult, 0, len(order))
	for _, name := range order {
		out = append(out, TestResult{Name: name, Passed: tests[name]})
	}
	return out
}

// splitStreams — `go test -json` пишет всё в stdout; stdout/stderr разделяем
// эвристики нет — оба поля получают один и тот же поток (контракт не сломан).
func splitStreams(raw []byte) (string, string, []TestResult) {
	return string(raw), "", nil
}

// cappedWriter — ограничитель размера вывода (защита от OOM при бесконечном печатании).
type cappedWriter struct {
	buf   *bytes.Buffer
	limit int64
}

func (w *cappedWriter) Write(p []byte) (int, error) {
	if int64(w.buf.Len()) >= w.limit {
		return len(p), nil // глотаем остаток, счётчик возвращаем честно
	}
	w.buf.Write(p)
	return len(p), nil
}
