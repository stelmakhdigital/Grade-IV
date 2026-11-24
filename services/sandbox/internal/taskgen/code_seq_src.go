package taskgen

// Шаблоны семьи code-seq: заготовка, эталон, тесты и мутанты для junior
// (k наибольших) и middle (k самых частых слов). Мутант — правка эталона в одном
// месте; маркер обязан встречаться в эталоне ровно один раз (mutantDrafts).

import (
	"fmt"
	"strings"
)

// ---------- junior: k наибольших ----------

const stubGoTopK = `package task

// TopK возвращает не более k наибольших элементов nums в порядке убывания.
func TopK(nums []int, k int) []int {
	panic("реализуйте TopK")
}
`

const refGoTopK = `package task

import "sort"

// TopK возвращает не более k наибольших элементов nums в порядке убывания.
func TopK(nums []int, k int) []int {
	if k <= 0 {
		return []int{}
	}
	out := append([]int{}, nums...)
	sort.Sort(sort.Reverse(sort.IntSlice(out)))
	if k > len(out) {
		k = len(out)
	}
	return out[:k]
}
`

var patchesGoTopK = []patch{
	{"не обрезать по k", "\treturn out[:k]\n", "\treturn out\n"},
	{"по возрастанию", "\tsort.Sort(sort.Reverse(sort.IntSlice(out)))\n", "\tsort.Sort(sort.IntSlice(out))\n"},
	{"k<=0 отдаёт весь массив", "\t\treturn []int{}\n", "\t\tout := append([]int{}, nums...)\n\t\tsort.Sort(sort.Reverse(sort.IntSlice(out)))\n\t\treturn out\n"},
	{"k наименьших вместо наибольших", "\treturn out[:k]\n", "\treturn out[len(out)-k:]\n"},
}

func testGoTopK(cases []caseK) string {
	var b strings.Builder
	b.WriteString("package task\n\nimport (\n\t\"reflect\"\n\t\"testing\"\n)\n\n")
	b.WriteString(noteTrustedGo)
	b.WriteString("func TestTopK(t *testing.T) {\n\tcases := []struct {\n\t\tnums []int\n\t\tk    int\n\t\twant []int\n\t}{\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("\t\t{%s, %d, %s},\n", goInts(c.nums), c.k, goInts(canonTopK(c.nums, c.k))))
	}
	b.WriteString(`	}
	for _, c := range cases {
		if got := TopK(c.nums, c.k); !reflect.DeepEqual(got, c.want) {
			t.Errorf("TopK(%v, %d) = %v, want %v", c.nums, c.k, got, c.want)
		}
	}
}
`)
	return b.String()
}

const stubPyTopK = `def top_k(nums, k):
    """Не более k наибольших элементов в порядке убывания."""
    raise NotImplementedError("реализуйте top_k")
`

const refPyTopK = `def top_k(nums, k):
    """Не более k наибольших элементов в порядке убывания."""
    if k <= 0:
        return []
    out = sorted(nums, reverse=True)
    return out[:k]
`

var patchesPyTopK = []patch{
	{"не обрезать по k", "    return out[:k]\n", "    return out\n"},
	{"по возрастанию", "    out = sorted(nums, reverse=True)\n", "    out = sorted(nums)\n"},
	{"проверка k убрана", "    if k <= 0:\n        return []\n", ""},
	{"повторы схлопнуты", "    out = sorted(nums, reverse=True)\n", "    out = sorted(set(nums), reverse=True)\n"},
}

func testPyTopK(cases []caseK) string {
	var b strings.Builder
	b.WriteString("import pytest\n\nfrom solution import top_k\n\n")
	b.WriteString(noteTrustedPy)
	b.WriteString("@pytest.mark.parametrize(\n    \"nums, k, want\",\n    [\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("        (%s, %d, %s),\n", pyInts(c.nums), c.k, pyInts(canonTopK(c.nums, c.k))))
	}
	b.WriteString("    ],\n)\ndef test_top_k(nums, k, want):\n    assert top_k(nums, k) == want\n")
	return b.String()
}

// ---------- middle: k самых частых слов ----------

const stubGoTopWords = `package task

// TopWords возвращает не более k самых частых слов: по убыванию частоты, при
// равной частоте — лексикографически по возрастанию.
func TopWords(words []string, k int) []string {
	panic("реализуйте TopWords")
}
`

const refGoTopWords = `package task

import "sort"

// TopWords возвращает не более k самых частых слов: по убыванию частоты, при
// равной частоте — лексикографически по возрастанию.
func TopWords(words []string, k int) []string {
	if k <= 0 {
		return []string{}
	}
	cnt := make(map[string]int, len(words))
	for _, w := range words {
		cnt[w]++
	}
	uniq := make([]string, 0, len(cnt))
	for w := range cnt {
		uniq = append(uniq, w)
	}
	sort.Slice(uniq, func(i, j int) bool {
		if cnt[uniq[i]] != cnt[uniq[j]] {
			return cnt[uniq[i]] > cnt[uniq[j]]
		}
		return uniq[i] < uniq[j]
	})
	if k > len(uniq) {
		k = len(uniq)
	}
	return uniq[:k]
}
`

var patchesGoTopWords = []patch{
	{"tie-break наоборот", "\t\treturn uniq[i] < uniq[j]\n", "\t\treturn uniq[i] > uniq[j]\n"},
	{"частота по возрастанию", "\t\t\treturn cnt[uniq[i]] > cnt[uniq[j]]\n", "\t\t\treturn cnt[uniq[i]] < cnt[uniq[j]]\n"},
	{"не обрезать по k", "\treturn uniq[:k]\n", "\treturn uniq\n"},
	{"частоты не считаются", "\t\tcnt[w]++\n", "\t\tcnt[w] = 1\n"},
}

func testGoTopWords(cases []caseW) string {
	var b strings.Builder
	b.WriteString("package task\n\nimport (\n\t\"reflect\"\n\t\"testing\"\n)\n\n")
	b.WriteString(noteTrustedGo)
	b.WriteString("func TestTopWords(t *testing.T) {\n\tcases := []struct {\n\t\twords []string\n\t\tk     int\n\t\twant  []string\n\t}{\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("\t\t{%s, %d, %s},\n", goStrs(c.words), c.k, goStrs(canonTopWords(c.words, c.k))))
	}
	b.WriteString(`	}
	for _, c := range cases {
		if got := TopWords(c.words, c.k); !reflect.DeepEqual(got, c.want) {
			t.Errorf("TopWords(%v, %d) = %v, want %v", c.words, c.k, got, c.want)
		}
	}
}
`)
	return b.String()
}

const stubPyTopWords = `def top_words(words, k):
    """Не более k самых частых слов: частота по убыванию, затем лексикографически."""
    raise NotImplementedError("реализуйте top_words")
`

const refPyTopWords = `def top_words(words, k):
    """Не более k самых частых слов: частота по убыванию, затем лексикографически."""
    if k <= 0:
        return []
    cnt = {}
    for w in words:
        cnt[w] = cnt.get(w, 0) + 1
    uniq = sorted(cnt, key=lambda w: (-cnt[w], w))
    return uniq[:k]
`

var patchesPyTopWords = []patch{
	{"частота по возрастанию", "    uniq = sorted(cnt, key=lambda w: (-cnt[w], w))\n", "    uniq = sorted(cnt, key=lambda w: (cnt[w], w))\n"},
	{"не обрезать по k", "    return uniq[:k]\n", "    return uniq\n"},
	{"частоты не считаются", "        cnt[w] = cnt.get(w, 0) + 1\n", "        cnt[w] = cnt.get(w, 0)\n"},
	{"без сортировки", "    uniq = sorted(cnt, key=lambda w: (-cnt[w], w))\n", "    uniq = list(cnt)\n"},
}

func testPyTopWords(cases []caseW) string {
	var b strings.Builder
	b.WriteString("import pytest\n\nfrom solution import top_words\n\n")
	b.WriteString(noteTrustedPy)
	b.WriteString("@pytest.mark.parametrize(\n    \"words, k, want\",\n    [\n")
	for _, c := range cases {
		b.WriteString(fmt.Sprintf("        (%s, %d, %s),\n", pyStrs(c.words), c.k, pyStrs(canonTopWords(c.words, c.k))))
	}
	b.WriteString("    ],\n)\ndef test_top_words(words, k, want):\n    assert top_words(words, k) == want\n")
	return b.String()
}
