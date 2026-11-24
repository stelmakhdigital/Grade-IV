package taskgen

// Семьи-шаблоны (уровень A, spec-24): детерминированный генератор, который по
// seed и грейду выдаёт условие, данные, чек банка, эталон и мутанты. LLM здесь
// не участвует: разнообразие дают параметры, а проверяет задача, написанная
// человеком. Один и тот же seed даёт байт-в-байт одну и ту же задачу.

import (
	"fmt"
	"math/rand"
	"sort"
	"strconv"
	"strings"
)

// Draft — черновик задачи до gate. Files — содержимое задачи (файлы кандидата +
// файлы градатора), Reference/Mutants — сдачи, на которых проверяется чек.
type Draft struct {
	Title     string
	Statement string
	Grades    []string
	Files     map[string]string
	Reference map[string]string
	Mutants   []map[string]string
}

// Family — семья шаблонов одного стека.
type Family struct {
	ID     string
	Stacks []string
	Grades []string
	Build  func(seed int64, grade string) (*Draft, error)
}

var registry = map[string]*Family{}

func register(f *Family) { registry[f.ID] = f }

// Families — id доступных семей (для админ-эндпоинта и пула).
func Families() []string {
	out := make([]string, 0, len(registry))
	for id := range registry {
		out = append(out, id)
	}
	sort.Strings(out)
	return out
}

// Build — собрать черновик задачи семьи под грейд.
func Build(id string, seed int64, grade string) (*Draft, error) {
	f, ok := registry[id]
	if !ok {
		return nil, fmt.Errorf("неизвестная семья %q (доступны: %s)", id, strings.Join(Families(), ", "))
	}
	for _, g := range f.Grades {
		if g == grade {
			return f.Build(seed, grade)
		}
	}
	return nil, fmt.Errorf("семья %s не даёт грейд %s", id, grade)
}

// StackOf — стек семьи (первый из заявленных).
func StackOf(id string) (string, bool) {
	f, ok := registry[id]
	if !ok || len(f.Stacks) == 0 {
		return "", false
	}
	return f.Stacks[0], true
}

// gradeLevel — возрастание сложности (junior=1 … staff=4).
func gradeLevel(grade string) int {
	switch grade {
	case "junior":
		return 1
	case "middle":
		return 2
	case "senior":
		return 3
	case "staff":
		return 4
	}
	return 0
}

// statsKeys — ключи вывода и их порядок по уровню. Порядок зафиксирован и в
// условии, и в чеке: решение обязано печатать ровно эти строки в этом порядке.
func statsKeys(level int) []string {
	keys := []string{"sum", "n", "avg"}
	if level >= 2 {
		keys = append(keys, "min", "max")
	}
	if level >= 3 {
		keys = append(keys, "median")
	}
	if level >= 4 {
		keys = append(keys, "mode")
	}
	return keys
}

// statsCase — сгенерированные данные.
type statsCase struct {
	nums  []int64
	cells []string // строки input.txt (с мусорными токенами)
}

// generateStats — данные с гарантиями, на которых мутанты обязаны отличаться:
// мусор (NA, пустые ячейки), отрицательные числа (middle+), чётное n с нечётной
// положительной медианной парой (senior+), перевязка mod (staff) и avg, у
// которого округление к ближнему отличается от округления к нулю.
func generateStats(seed int64, level int) statsCase {
	rng := rand.New(rand.NewSource(seed))
	lo, hi := int64(1), int64(40)
	if level >= 2 {
		lo, hi = -25, 40
	}
	if level >= 3 {
		lo, hi = -60, 120
	}

	count := 12 + rng.Intn(8) // 12..19 значений
	nums := make([]int64, 0, count+16)
	for i := 0; i < count; i++ {
		nums = append(nums, lo+rng.Int63n(hi-lo+1))
	}
	// Отрицательное число обязательно с middle: иначе мутант «min ищется как
	// максимум» отличался бы не на всех данных.
	if level >= 2 && !hasNegative(nums) {
		nums[0] = -(1 + rng.Int63n(20))
	}
	if level >= 4 {
		nums = fixModeTie(rng, nums)
	}
	if level >= 3 && len(nums)%2 == 1 {
		nums = append(nums, absentValue(rng, nums, 200))
	}
	// Два «подкручиваемых» значения: на них ремонтируется avg, поэтому частоты
	// настоящих значений (а значит и mod-перевязка) не плывут.
	x := absentValue(rng, nums, 900)
	y := absentValue(rng, append(append([]int64(nil), nums...), x), 1300)
	nums = append(nums, x, y)
	nums = repairStatsInvariants(nums, level)
	return statsCase{nums: nums, cells: rebuildCells(rng, nums, level)}
}

// repairStatsInvariants — добивает свойства данных: медианная пара (senior+)
// нечётная и положительная, avg отличается при округлении к ближнему (все
// уровни), min != max. Медианную пару двигает по правому её элементу, avg —
// максимумом: максимум в пару не входит и не сдвигает ни минимум, ни mod.
func repairStatsInvariants(nums []int64, level int) []int64 {
	sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
	n := len(nums)
	if nums[0] == nums[n-1] {
		nums[n-1] += 7
	}
	if level >= 3 {
		mid := n / 2
		for i := 0; i < 200; i++ {
			pair := nums[mid-1] + nums[mid]
			if pair > 0 && pair%2 != 0 {
				break
			}
			nums[mid]++
			sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
		}
	}
	for i := 0; i < 4*n+64; i++ {
		if avgRoundDiffers(nums) {
			return nums
		}
		nums[n-1]++
	}
	return nums
}

// avgRoundDiffers — даёт ли округление к ближнему (мутант) другой результат,
// чем целочисленное деление bash (округление к нулю).
func avgRoundDiffers(nums []int64) bool {
	var sum int64
	for _, v := range nums {
		sum += v
	}
	n := int64(len(nums))
	trunc := sum / n
	abs := sum
	if abs < 0 {
		abs = -abs
	}
	q := (2*abs + n) / (2 * n) // half-up, как в мутанте на awk
	if sum < 0 {
		q = -q
	}
	return q != trunc
}

// absentValue — значение, которого в данных нет (для mod-вершин и добивки чётности).
func absentValue(rng *rand.Rand, nums []int64, base int64) int64 {
	have := make(map[int64]bool, len(nums))
	for _, v := range nums {
		have[v] = true
	}
	for {
		v := base + rng.Int63n(400)
		if !have[v] {
			return v
		}
	}
}

func separator(rng *rand.Rand) string {
	switch rng.Intn(3) {
	case 1:
		return ","
	case 2:
		return "\t"
	}
	return " "
}

func hasNegative(nums []int64) bool {
	for _, v := range nums {
		if v < 0 {
			return true
		}
	}
	return false
}

// fixModeTie — две вершины с одинаковой частотой, выше которой ни у кого нет:
// тогда «при перевязке бери большее» отличается гарантированно.
func fixModeTie(rng *rand.Rand, nums []int64) []int64 {
	cnt := map[int64]int{}
	maxc := 0
	for _, v := range nums {
		cnt[v]++
		if cnt[v] > maxc {
			maxc = cnt[v]
		}
	}
	rep := maxc + 1
	a := absentValue(rng, nums, 500)
	b := absentValue(rng, append(append([]int64(nil), nums...), a), 500)
	out := append([]int64(nil), nums...)
	for i := 0; i < rep; i++ {
		out = append(out, a, b)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}

// rebuildCells — раскладка чисел по строкам с мусором (после правок nums).
func rebuildCells(rng *rand.Rand, nums []int64, level int) []string {
	rows := 4 + rng.Intn(3)
	per := len(nums)/rows + 1
	cells := make([]string, 0, rows)
	for i := 0; i < len(nums); i += per {
		end := i + per
		if end > len(nums) {
			end = len(nums)
		}
		toks := make([]string, 0, end-i+2)
		for _, v := range nums[i:end] {
			toks = append(toks, strconv.FormatInt(v, 10))
			if rng.Intn(5) == 0 {
				toks = append(toks, "") // пустая ячейка: два разделителя подряд
			}
		}
		toks = append(toks, "NA")
		cells = append(cells, strings.Join(toks, separator(rng)))
	}
	return cells
}

// statsExpected — ожидаемые значения считаются здесь, а не в bash: чек сверяется
// с ними, эталонный bash — с чеком. Разойдёмся — gate не пропустит задачу.
func statsExpected(nums []int64, level int) map[string]string {
	n := len(nums)
	var sum, min, max int64
	min, max = nums[0], nums[0]
	for _, v := range nums {
		sum += v
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	exp := map[string]string{
		"sum": strconv.FormatInt(sum, 10),
		"n":   strconv.Itoa(n),
		"avg": strconv.FormatInt(sum/int64(n), 10), // как в bash: округление к нулю
	}
	if level >= 2 {
		exp["min"] = strconv.FormatInt(min, 10)
		exp["max"] = strconv.FormatInt(max, 10)
	}
	if level >= 3 {
		s := append([]int64(nil), nums...)
		sort.Slice(s, func(i, j int) bool { return s[i] < s[j] })
		var med int64
		if n%2 == 1 {
			med = s[n/2]
		} else {
			med = (s[n/2-1] + s[n/2]) / 2
		}
		exp["median"] = strconv.FormatInt(med, 10)
	}
	if level >= 4 {
		cnt := map[int64]int{}
		for _, v := range nums {
			cnt[v]++
		}
		keys := make([]int64, 0, len(cnt))
		for k := range cnt {
			keys = append(keys, k)
		}
		sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
		best, bestC := keys[0], 0
		for _, k := range keys { // перебор по возрастанию: при перевязке остаётся меньшее
			if cnt[k] > bestC {
				best, bestC = k, cnt[k]
			}
		}
		exp["mode"] = strconv.FormatInt(best, 10)
	}
	return exp
}
