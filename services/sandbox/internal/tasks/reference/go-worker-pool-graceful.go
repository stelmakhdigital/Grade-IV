//go:build ignore

package task

import (
	"context"
	"errors"
	"sync"
)

// ErrPoolClosed — Submit после Close.
var ErrPoolClosed = errors.New("pool: закрыта")

// Result — результат джобы.
type Result struct {
	Value any
	Err   error
}

type job struct {
	f   func() (any, error)
	out chan Result
}

// Pool — пул воркеров с graceful shutdown.
type Pool struct {
	queue   chan job
	wg      sync.WaitGroup
	mu      sync.Mutex
	closed  bool
	workers int
}

// NewPool создаёт пул из n воркеров (n < 1 → 1).
func NewPool(n int) *Pool {
	if n < 1 {
		n = 1
	}
	p := &Pool{queue: make(chan job), workers: n}
	p.wg.Add(n)
	for i := 0; i < n; i++ {
		go p.worker()
	}
	return p
}

func (p *Pool) worker() {
	defer p.wg.Done()
	for j := range p.queue {
		v, err := j.f()
		j.out <- Result{Value: v, Err: err}
	}
}

// Submit ставит джобу в очередь и возвращает канал с одним Result.
// Если ctx уже отменён — джоба не выполняется, канал получает {Err: ctx.Err()}.
// После Close — {Err: ErrPoolClosed}.
func (p *Pool) Submit(ctx context.Context, f func() (any, error)) <-chan Result {
	out := make(chan Result, 1)
	select {
	case <-ctx.Done():
		out <- Result{Err: ctx.Err()}
		return out
	default:
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	if p.closed {
		out <- Result{Err: ErrPoolClosed}
		return out
	}
	p.queue <- job{f: f, out: out}
	return out
}

// Close запрещает новые Submit и дожидается завершения всех поставленных джоб
// (drain), затем останавливает воркеров.
func (p *Pool) Close() {
	p.mu.Lock()
	if p.closed {
		p.mu.Unlock()
		return
	}
	p.closed = true
	close(p.queue)
	p.mu.Unlock()
	p.wg.Wait()
}
