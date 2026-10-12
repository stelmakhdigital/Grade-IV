//go:build ignore

package task

// RingBuffer — очередь на кольцевом буфере фиксированной ёмкости.
type RingBuffer struct {
	buf  []int
	head int // индекс чтения
	tail int // индекс записи
	n    int // текущий размер
	cap  int
}

// NewRing создаёт буфер ёмкостью size (size >= 0).
func NewRing(size int) *RingBuffer {
	if size < 0 {
		size = 0
	}
	return &RingBuffer{buf: make([]int, size), cap: size}
}

// Push добавляет v в конец; false, если буфер полон.
func (r *RingBuffer) Push(v int) bool {
	if r.n == r.cap {
		return false
	}
	r.buf[r.tail] = v
	r.tail = (r.tail + 1) % r.cap
	r.n++
	return true
}

// Pop извлекает из начала; (0, false), если пусто.
func (r *RingBuffer) Pop() (int, bool) {
	if r.n == 0 {
		return 0, false
	}
	v := r.buf[r.head]
	r.head = (r.head + 1) % r.cap
	r.n--
	return v, true
}

// Len — текущий размер.
func (r *RingBuffer) Len() int {
	return r.n
}
