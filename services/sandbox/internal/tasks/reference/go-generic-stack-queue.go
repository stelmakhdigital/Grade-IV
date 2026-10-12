//go:build ignore

package task

// Stack — LIFO-стек на generics.
type Stack[T any] struct {
	data []T
}

func NewStack[T any]() *Stack[T] { return &Stack[T]{} }
func (s *Stack[T]) Push(v T)     { s.data = append(s.data, v) }
func (s *Stack[T]) Len() int     { return len(s.data) }
func (s *Stack[T]) Top() (T, bool) {
	if len(s.data) == 0 {
		return *new(T), false
	}
	return s.data[len(s.data)-1], true
}
func (s *Stack[T]) Pop() (T, bool) {
	if len(s.data) == 0 {
		return *new(T), false
	}
	v := s.data[len(s.data)-1]
	s.data = s.data[:len(s.data)-1]
	return v, true
}

// Queue — FIFO-очередь на generics.
type Queue[T any] struct {
	data []T
	head int
}

func NewQueue[T any]() *Queue[T] { return &Queue[T]{} }
func (q *Queue[T]) Enqueue(v T)  { q.data = append(q.data, v) }
func (q *Queue[T]) Len() int     { return len(q.data) - q.head }
func (q *Queue[T]) Dequeue() (T, bool) {
	if q.Len() == 0 {
		return *new(T), false
	}
	v := q.data[q.head]
	q.head++
	if q.head == len(q.data) {
		q.data = nil
		q.head = 0
	}
	return v, true
}
