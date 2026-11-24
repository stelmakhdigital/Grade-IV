package taskgen

import (
	"sort"
	"testing"
)

// TestShellStatsInvariants — свойства данных, на которых мутанты обязаны
// ловиться. Проверяются напрямую по генератору: если свойство не выполнено,
// gate отбракует задачу (или, хуже, пропустит тихий мутант).
func TestShellStatsInvariants(t *testing.T) {
	for _, grade := range []string{"junior", "middle", "senior", "staff"} {
		level := gradeLevel(grade)
		for seed := int64(1); seed <= 60; seed++ {
			cs := generateStats(seed, level)
			nums := append([]int64(nil), cs.nums...)
			sort.Slice(nums, func(i, j int) bool { return nums[i] < nums[j] })
			n := len(nums)

			if n < 4 {
				t.Fatalf("%s/%d: значений всего %d", grade, seed, n)
			}
			if nums[0] == nums[n-1] {
				t.Errorf("%s/%d: все значения равны (%d) — min/max-мутант был бы тихим", grade, seed, nums[0])
			}
			if !avgRoundDiffers(nums) {
				t.Errorf("%s/%d: округление avg к ближнему совпадает с округлением к нулю: nums=%v", grade, seed, nums)
			}
			if level >= 2 && !hasNegative(nums) {
				t.Errorf("%s/%d: нет отрицательных значений: %v", grade, seed, nums)
			}
			if level >= 3 {
				if n%2 != 0 {
					t.Errorf("%s/%d: нечётное n=%d — медиана «верхняя» неотличима", grade, seed, n)
				} else {
					pair := nums[n/2-1] + nums[n/2]
					if pair <= 0 || pair%2 == 0 {
						t.Errorf("%s/%d: медианная пара %d (нужна нечётная и > 0): %v", grade, seed, pair, nums)
					}
				}
			}
			if level >= 4 {
				mode := modeOf(nums)
				cnt := counts(nums)
				second := 0
				for _, c := range cnt {
					if c > cnt[mode] && c > second {
						second = c
					}
				}
				if second != 0 {
					t.Errorf("%s/%d: mode=%d не вершина (есть частота %d): %v", grade, seed, mode, second, nums)
				}
				ties := 0
				for _, c := range cnt {
					if c == cnt[mode] {
						ties++
					}
				}
				if ties < 2 {
					t.Errorf("%s/%d: перевязки mod нет (частota %d у одного): %v", grade, seed, cnt[mode], nums)
				}
			}
		}
	}
}

func counts(nums []int64) map[int64]int {
	c := map[int64]int{}
	for _, v := range nums {
		c[v]++
	}
	return c
}

func modeOf(nums []int64) int64 {
	c := counts(nums)
	keys := make([]int64, 0, len(c))
	for k := range c {
		keys = append(keys, k)
	}
	sort.Slice(keys, func(i, j int) bool { return keys[i] < keys[j] })
	best, bestC := keys[0], 0
	for _, k := range keys {
		if c[k] > bestC {
			best, bestC = k, c[k]
		}
	}
	return best
}
