//go:build ignore

package task

import "sync"

// MapStage — стадия пайплайна из n воркеров: применяет f к каждому значению in
// и возвращает канал с результатами В ИСХОДНОМ ПОРЯДКЕ. Когда in закрыт, out
// закрывается после доставки всех значений.
func MapStage(n int, in <-chan int, f func(int) int) <-chan int {
	if n < 1 {
		n = 1
	}
	type item struct {
		idx int
		v   int
	}
	out := make(chan int)
	tagged := make(chan item)
	go func() {
		defer close(tagged)
		i := 0
		for v := range in {
			tagged <- item{i, v}
			i++
		}
	}()
	results := make(chan item)
	var wg sync.WaitGroup
	for w := 0; w < n; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for it := range tagged {
				results <- item{it.idx, f(it.v)}
			}
		}()
	}
	go func() {
		wg.Wait()
		close(results)
	}()
	go func() {
		defer close(out)
		next := 0
		buf := map[int]int{}
		for r := range results {
			buf[r.idx] = r.v
			for {
				v, ok := buf[next]
				if !ok {
					break
				}
				delete(buf, next)
				out <- v
				next++
			}
		}
	}()
	return out
}
