//go:build ignore

package task

// Middleware оборачивает следующий шаг цепочки.
type Middleware func(next func())

// Chain — цепочка middleware.
type Chain struct {
	mws []Middleware
}

func NewChain() *Chain { return &Chain{} }

// Use добавляет middleware и возвращает c (для цепочки).
func (c *Chain) Use(mw Middleware) *Chain {
	c.mws = append(c.mws, mw)
	return c
}

// Run запускает цепочку (первый Use — внешняя обёртка).
func (c *Chain) Run() {
	var run func(i int)
	run = func(i int) {
		if i >= len(c.mws) {
			return
		}
		c.mws[i](func() { run(i + 1) })
	}
	run(0)
}
