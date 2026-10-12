//go:build ignore

package task

var romanVal = map[byte]int{'I': 1, 'V': 5, 'X': 10, 'L': 50, 'C': 100, 'D': 500, 'M': 1000}

// RomanToInt переводит римскую запись в целое число.
func RomanToInt(s string) int {
	sum := 0
	for i := 0; i < len(s); i++ {
		cur := romanVal[s[i]]
		if cur == 0 {
			continue
		}
		if i+1 < len(s) && romanVal[s[i+1]] > cur {
			sum -= cur
		} else {
			sum += cur
		}
	}
	return sum
}
