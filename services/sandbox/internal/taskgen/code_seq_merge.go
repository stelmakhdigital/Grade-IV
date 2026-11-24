package taskgen

// Шаблоны семьи code-seq для senior: объединение интервалов. Мутанты бьют по
// трём местам, где это обычно и ломают: склейка соседних, вложенный отрезок,
// сортировка входа.

import (
	"fmt"
	"strings"
)

const stubGoMerge = `package task

// MergeIntervals объединяет пересекающиеся и соседние отрезки [a, b] (a <= b).
func MergeIntervals(pairs [][2]int) [][2]int {
	panic("реализуйте MergeIntervals")
}
`

const refGoMerge = `package task

import "sort"

// MergeIntervals объединяет пересекающиеся и соседние отрезки [a, b] (a <= b).
func MergeIntervals(pairs [][2]int) [][2]int {
	if len(pairs) == 0 {
		return [][2]int{}
	}
	p := append([][2]int{}, pairs...)
	sort.Slice(p, func(i, j int) bool {
		if p[i][0] != p[j][0] {
			return p[i][0] < p[j][0]
		}
		return p[i][1] < p[j][1]
	})
	out := [][2]int{p[0]}
	for _, cur := range p[1:] {
		last := &out[len(out)-1]
		if cur[0] <= last[1]+1 {
			if cur[1] > last[1] {
				last[1] = cur[1]
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}
`

var patchesGoMerge = []patch{
	{"соседние не склеиваются", "\t\tif cur[0] <= last[1]+1 {\n", "\t\tif cur[0] <= last[1] {\n"},
	{"вложенный отрезок сужает результат", "\t\t\tif cur[1] > last[1] {\n\t\t\t\tlast[1] = cur[1]\n\t\t\t}\n", "\t\t\tlast[1] = cur[1]\n"},
	{"сортировка наоборот", "\t\t\treturn p[i][0] < p[j][0]\n", "\t\t\treturn p[i][0] > p[j][0]\n"},
	{"первый отрезок теряется", "\tout := [][2]int{p[0]}\n", "\tout := [][2]int{}\n"},
}

func testGoMerge(cases []caseI) string {
	var b strings.Builder
	b.WriteString("package task\n\nimport (\n\t\"reflect\"\n\t\"testing\"\n)\n\n")
	b.WriteString(noteTrustedGo)
	b.WriteString("func TestMergeIntervals(t *testing.T) {\n\tcases := []struct {\n\t\tin   [][2]int\n\t\twant [][2]int\n\t}{\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("\t\t{%s, %s},\n", goPairs(c.in), goPairs(canonMergeIntervals(c.in))))
	}
	b.WriteString(`	}
	for _, c := range cases {
		if got := MergeIntervals(c.in); !reflect.DeepEqual(got, c.want) {
			t.Errorf("MergeIntervals(%v) = %v, want %v", c.in, got, c.want)
		}
	}
}
`)
	return b.String()
}

const stubPyMerge = `def merge_intervals(pairs):
    """Объединение пересекающихся и соседних отрезков; список кортежей."""
    raise NotImplementedError("реализуйте merge_intervals")
`

const refPyMerge = `def merge_intervals(pairs):
    """Объединение пересекающихся и соседних отрезков; список кортежей."""
    if not pairs:
        return []
    p = sorted(pairs)
    out = [list(p[0])]
    for a, b in p[1:]:
        if a <= out[-1][1] + 1:
            out[-1][1] = max(out[-1][1], b)
        else:
            out.append([a, b])
    return [tuple(x) for x in out]
`

var patchesPyMerge = []patch{
	{"соседние не склеиваются", "        if a <= out[-1][1] + 1:\n", "        if a <= out[-1][1]:\n"},
	{"вложенный отрезок сужает результат", "            out[-1][1] = max(out[-1][1], b)\n", "            out[-1][1] = b\n"},
	{"вход не сортируется", "    p = sorted(pairs)\n", "    p = list(pairs)\n"},
	{"второй отрезок теряется", "    for a, b in p[1:]:\n", "    for a, b in p[2:]:\n"},
}

func testPyMerge(cases []caseI) string {
	var b strings.Builder
	b.WriteString("import pytest\n\nfrom solution import merge_intervals\n\n")
	b.WriteString(noteTrustedPy)
	b.WriteString("@pytest.mark.parametrize(\n    \"pairs, want\",\n    [\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("        (%s, %s),\n", pyPairs(c.in), pyPairs(canonMergeIntervals(c.in))))
	}
	b.WriteString("    ],\n)\ndef test_merge_intervals(pairs, want):\n    assert merge_intervals(pairs) == want\n")
	return b.String()
}
