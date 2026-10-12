//go:build ignore

package task

// IntHeap — минимальная куча целых (min-heap).
type IntHeap struct {
	data []int
}

func NewIntHeap() *IntHeap { return &IntHeap{} }

func (h *IntHeap) Len() int { return len(h.data) }

func (h *IntHeap) Push(v int) {
	h.data = append(h.data, v)
	h.up(len(h.data) - 1)
}

func (h *IntHeap) Pop() (int, bool) {
	if len(h.data) == 0 {
		return 0, false
	}
	min := h.data[0]
	h.data[0] = h.data[len(h.data)-1]
	h.data = h.data[:len(h.data)-1]
	if len(h.data) > 0 {
		h.down(0)
	}
	return min, true
}

func (h *IntHeap) Peek() (int, bool) {
	if len(h.data) == 0 {
		return 0, false
	}
	return h.data[0], true
}

func (h *IntHeap) up(i int) {
	for i > 0 {
		p := (i - 1) / 2
		if h.data[p] <= h.data[i] {
			break
		}
		h.data[p], h.data[i] = h.data[i], h.data[p]
		i = p
	}
}

func (h *IntHeap) down(i int) {
	n := len(h.data)
	for {
		l, r := 2*i+1, 2*i+2
		smallest := i
		if l < n && h.data[l] < h.data[smallest] {
			smallest = l
		}
		if r < n && h.data[r] < h.data[smallest] {
			smallest = r
		}
		if smallest == i {
			break
		}
		h.data[i], h.data[smallest] = h.data[smallest], h.data[i]
		i = smallest
	}
}
