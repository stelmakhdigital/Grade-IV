package taskgen

// Семья code-seq (spec-24): механика «обработать последовательность» на go и
// python, разная глубина по грейдам:
//
//	junior  — k наибольших по убыванию
//	middle  — k самых частых слов с лексикографическим tie-break
//	senior  — объединение перекрывающихся и соседних отрезков
//
// Семья одна по смыслу, но регистрируется дважды (code-seq-go, code-seq-py):
// черновик — один набор файлов под один стек, а язык задачи семья отдаёт через
// Stacks[0] (taskgen.StackOf).
//
// Ожидания в тестах считает канонический алгоритм на Go — здесь же, в
// генераторе. Эталон, который уедет в банк, — отдельный шаблон на языке задачи:
// если он разойдётся с каноническим алгоритмом, задача не пройдёт gate и
// останется в логе с причиной. Тихих «зелёных для всех» тестов тут не бывает:
// gate требует, чтобы эталон прошёл, а каждый мутант упал.

import (
	"fmt"
	"math/rand"
	"sort"
	"strings"
)

// codeMaxMutants — меньше, чем лимит gate: на go/python каждый мутант — полная
// сборка, а попыток генерации за тик несколько.
const codeMaxMutants = 4

func init() {
	for _, e := range []struct{ id, stack string }{
		{"code-seq-go", "go"},
		{"code-seq-py", "python"},
	} {
		stack := e.stack
		register(&Family{
			ID:     e.id,
			Stacks: []string{stack},
			Grades: []string{"junior", "middle", "senior"},
			Build: func(seed int64, grade string) (*Draft, error) {
				return buildCodeSeq(seed, grade, stack)
			},
		})
	}
}

// ---------- канонические алгоритмы: по ним считаются ожидания тестов ----------

func canonTopK(nums []int, k int) []int {
	if k <= 0 {
		return []int{}
	}
	out := append([]int{}, nums...)
	sort.Slice(out, func(i, j int) bool { return out[i] > out[j] })
	if k > len(out) {
		k = len(out)
	}
	return out[:k]
}

func canonTopWords(words []string, k int) []string {
	if k <= 0 {
		return []string{}
	}
	cnt := map[string]int{}
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
		return uniq[i] < uniq[j] // как sort.Strings и как sorted() в python
	})
	if k > len(uniq) {
		k = len(uniq)
	}
	return uniq[:k]
}

type interval struct{ a, b int }

func canonMergeIntervals(in []interval) []interval {
	if len(in) == 0 {
		return []interval{}
	}
	p := append([]interval{}, in...)
	sort.Slice(p, func(i, j int) bool {
		if p[i].a != p[j].a {
			return p[i].a < p[j].a
		}
		return p[i].b < p[j].b
	})
	out := []interval{p[0]}
	for _, cur := range p[1:] {
		last := &out[len(out)-1]
		if cur.a <= last.b+1 { // соседние [1,2] и [3,4] тоже склеиваем
			if cur.b > last.b {
				last.b = cur.b
			}
			continue
		}
		out = append(out, cur)
	}
	return out
}

// ---------- сборка черновика ----------

func buildCodeSeq(seed int64, grade, stack string) (*Draft, error) {
	rnd := rand.New(rand.NewSource(seed))
	switch grade {
	case "junior":
		if stack == "go" {
			return assemble("k наибольших", stmtTopK(), grade,
				map[string]string{"go.mod": goMod, "solution.go": stubGoTopK, "solution_test.go": testGoTopK(topKCases(rnd))},
				"solution.go", refGoTopK, patchesGoTopK)
		}
		return assemble("k наибольших", stmtTopK(), grade,
			map[string]string{"solution.py": stubPyTopK, "test_solution.py": testPyTopK(topKCases(rnd))},
			"solution.py", refPyTopK, patchesPyTopK)
	case "middle":
		if stack == "go" {
			return assemble("k самых частых слов", stmtTopWords(), grade,
				map[string]string{"go.mod": goMod, "solution.go": stubGoTopWords, "solution_test.go": testGoTopWords(topWordsCases(rnd))},
				"solution.go", refGoTopWords, patchesGoTopWords)
		}
		return assemble("k самых частых слов", stmtTopWords(), grade,
			map[string]string{"solution.py": stubPyTopWords, "test_solution.py": testPyTopWords(topWordsCases(rnd))},
			"solution.py", refPyTopWords, patchesPyTopWords)
	case "senior":
		if stack == "go" {
			return assemble("Объединить интервалы", stmtMerge(), grade,
				map[string]string{"go.mod": goMod, "solution.go": stubGoMerge, "solution_test.go": testGoMerge(mergeCases(rnd))},
				"solution.go", refGoMerge, patchesGoMerge)
		}
		return assemble("Объединить интервалы", stmtMerge(), grade,
			map[string]string{"solution.py": stubPyMerge, "test_solution.py": testPyMerge(mergeCases(rnd))},
			"solution.py", refPyMerge, patchesPyMerge)
	default:
		return nil, fmt.Errorf("семья code-seq не даёт грейд %q", grade)
	}
}

// assemble — черновик из эталона и его мутантов.
func assemble(title, statement, grade string, files map[string]string, file, ref string, patches []patch) (*Draft, error) {
	muts, err := mutantDrafts(file, ref, patches)
	if err != nil {
		return nil, err
	}
	return &Draft{
		Title:     title,
		Statement: statement,
		Grades:    []string{grade},
		Files:     files,
		Reference: map[string]string{file: ref},
		Mutants:   muts,
	}, nil
}

const goMod = "module task\n\ngo 1.21\n"

// mutantDrafts — правки эталона в одном месте. Если маркер встречается не ровно
// один раз или правка не изменила текст — ошибка сборки черновика, а не тихий
// «мутант, который не мутант».
func mutantDrafts(file, ref string, patches []patch) ([]map[string]string, error) {
	if len(patches) > codeMaxMutants {
		patches = patches[:codeMaxMutants]
	}
	out := make([]map[string]string, 0, len(patches))
	for _, p := range patches {
		if n := strings.Count(ref, p.old); n != 1 {
			return nil, fmt.Errorf("мутант %s: маркер встречается %d раз, нужен ровно 1", p.name, n)
		}
		mut := strings.Replace(ref, p.old, p.new, 1)
		if mut == ref {
			return nil, fmt.Errorf("мутант %s: правка не изменила эталон", p.name)
		}
		out = append(out, map[string]string{file: mut})
	}
	return out, nil
}

type patch struct {
	name string
	old  string
	new  string
}

// ---------- условия (то, что видит кандидат) ----------

func stmtTopK() string {
	return `Напишите функцию, которая возвращает не более k наибольших элементов последовательности в порядке убывания.

Правила:
- k <= 0 — пустой результат;
- k больше длины — вся последовательность, отсортированная по убыванию;
- повторы не удалять: если максимум встречается несколько раз, он столько раз и попадает в ответ;
- пустой вход — пустой результат; вход можно менять на месте.`
}

func stmtTopWords() string {
	return `Напишите функцию, которая возвращает не более k самых частых слов: по убыванию частоты, а при равной частоте — в лексикографическом порядке по возрастанию (как sort.Strings / sorted()).

Правила:
- k <= 0 — пустой результат;
- k больше числа различных слов — все различные слова;
- регистр значим: "Go" и "go" — разные слова;
- пустой вход — пустой результат.`
}

func stmtMerge() string {
	return `Напишите функцию, которая объединяет отрезки [a, b] (a <= b на входе): пересекающиеся и соседние склеиваются в один, результат отсортирован по началу отрезка.

Правила:
- [1,3] и [3,5] пересекаются → [1,5];
- [1,2] и [3,4] соседние → [1,4];
- [1,10] и [2,3] → [1,10] (вложенный отрезок ничего не меняет);
- пустой вход — пустой результат; вход можно менять на месте.`
}

// ---------- данные тестов ----------

type caseK struct {
	nums []int
	k    int
}

type caseW struct {
	words []string
	k     int
}

type caseI struct{ in []interval }

// topKCases — случайное наполнение плюс фиксированные края: k=0, отрицательный
// k, k больше длины, пустой вход, все равные, только отрицательные. Случайный
// набор обязательно содержит повторы максимума — иначе мутант «не обрезать по k»
// мог бы пройти.
func topKCases(rnd *rand.Rand) []caseK {
	n := 8 + rnd.Intn(5)
	nums := make([]int, n)
	for i := range nums {
		nums[i] = rnd.Intn(41) - 20
	}
	maxv := nums[0]
	for _, x := range nums {
		if x > maxv {
			maxv = x
		}
	}
	nums[n-1], nums[n-2], nums[n-3] = maxv, maxv, -25
	return []caseK{
		{nums: nums, k: 3},
		{nums: nums, k: 1},
		{nums: nums, k: 0},
		{nums: nums, k: -2},
		{nums: nums, k: len(nums)},
		{nums: nums, k: len(nums) + 4},
		{nums: []int{}, k: 3},
		{nums: []int{7}, k: 1},
		{nums: []int{-4, -4, -9, -1}, k: 2},
		{nums: []int{5, 5, 5, 5}, k: 2},
	}
}

var wordAlphabet = []string{"архив", "бэкап", "лог", "монитор", "кэш", "сеть", "токен", "трафик"}

// topWordsCases — набор слов с заведомыми ничейными частотами (tie-break
// проверяется всегда) и краями по k.
func topWordsCases(rnd *rand.Rand) []caseW {
	base := []string{}
	for i := 0; i < 6+rnd.Intn(6); i++ {
		base = append(base, wordAlphabet[rnd.Intn(len(wordAlphabet))])
	}
	// две слова с одинаковой частотой, причём «ю» лексикографически позже «у»
	base = append(base, "узел", "узел", "узел", "узел", "узел", "юнит", "юнит", "юнит", "юнит", "юнит")
	rnd.Shuffle(len(base), func(i, j int) { base[i], base[j] = base[j], base[i] })
	return []caseW{
		{words: base, k: 3},
		{words: base, k: 1},
		{words: base, k: 0},
		{words: base, k: -1},
		{words: base, k: 100},
		{words: []string{}, k: 2},
		{words: []string{"go", "Go", "go"}, k: 1},
		{words: []string{"eta", "alpha", "beta"}, k: 2},
		{words: []string{"x"}, k: 5},
	}
}

// mergeCases — случайные отрезки поверх фиксированных форм: перекрытие,
// соседство, вложенность, дубликат. Вход перемешивается: решение обязано само
// сортировать.
func mergeCases(rnd *rand.Rand) []caseI {
	in := []interval{
		{100, 105}, {103, 110}, // перекрываются
		{200, 210}, {211, 215}, // соседние
		{300, 350}, {305, 310}, // вложенный
		{400, 400}, {400, 400}, // дубликат точки
	}
	for i := 0; i < 5+rnd.Intn(4); i++ {
		a := rnd.Intn(60)
		in = append(in, interval{a, a + rnd.Intn(8)})
	}
	rnd.Shuffle(len(in), func(i, j int) { in[i], in[j] = in[j], in[i] })
	return []caseI{
		{in: in},
		{in: []interval{}},
		{in: []interval{{5, 5}}},
		{in: []interval{{1, 3}, {2, 4}}},
		{in: []interval{{9, 12}, {0, 2}, {13, 14}}},
		{in: []interval{{1, 2}, {4, 5}}},
		{in: []interval{{7, 9}, {1, 2}, {3, 4}, {7, 8}}},
	}
}

// ---------- литералы ----------

const noteTrustedGo = "// Файл банка (доверенный): подмешивается в пакет на время сборки,\n" +
	"// в каталоге решения этого файла нет.\n"

const noteTrustedPy = "# Файл банка (доверенный): запускается из каталога градатора,\n" +
	"# кандидат его не задаёт.\n"

func goInts(v []int) string {
	if len(v) == 0 {
		return "[]int{}"
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return "[]int{" + strings.Join(parts, ", ") + "}"
}

func pyInts(v []int) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, x := range v {
		parts[i] = fmt.Sprint(x)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

func goStrs(v []string) string {
	if len(v) == 0 {
		return "[]string{}"
	}
	parts := make([]string, len(v))
	for i, s := range v {
		parts[i] = fmt.Sprintf("%q", s)
	}
	return "[]string{" + strings.Join(parts, ", ") + "}"
}

func pyStrs(v []string) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, s := range v {
		parts[i] = pyLiteral(s)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}

// pyLiteral — строка в python-кавычках. Go %q достаточно близок (dvoichnye
// kavychki, backslash-escapes), но обратный слэш и кавычки в наших данных не
// появляются, так что различий нет.
func pyLiteral(s string) string {
	return `"` + s + `"`
}

func goPairs(v []interval) string {
	if len(v) == 0 {
		return "[][2]int{}"
	}
	parts := make([]string, len(v))
	for i, p := range v {
		parts[i] = fmt.Sprintf("{%d, %d}", p.a, p.b)
	}
	return "[][2]int{" + strings.Join(parts, ", ") + "}"
}

func pyPairs(v []interval) string {
	if len(v) == 0 {
		return "[]"
	}
	parts := make([]string, len(v))
	for i, p := range v {
		parts[i] = fmt.Sprintf("(%d, %d)", p.a, p.b)
	}
	return "[" + strings.Join(parts, ", ") + "]"
}
