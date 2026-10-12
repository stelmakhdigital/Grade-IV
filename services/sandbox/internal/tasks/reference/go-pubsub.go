//go:build ignore

package task

import "sync"

// sub — одна подписка.
type sub struct {
	id string
	ch chan string
}

// Broker — in-memory pub/sub без goroutine.
type Broker struct {
	mu   sync.Mutex
	subs map[string]map[string]*sub // topic -> subID -> sub
	seq  int
}

func NewBroker() *Broker {
	return &Broker{subs: map[string]map[string]*sub{}}
}

// Subscribe возвращает id подписки и канал-подписку (buffered, ёмкость 16).
func (b *Broker) Subscribe(topic string) (string, <-chan string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	id := "s" + itoa(b.seq)
	ch := make(chan string, 16)
	if b.subs[topic] == nil {
		b.subs[topic] = map[string]*sub{}
	}
	b.subs[topic][id] = &sub{id: id, ch: ch}
	return id, ch
}

// Unsubscribe снимает подписку по id.
func (b *Broker) Unsubscribe(subID string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	for topic, m := range b.subs {
		if _, ok := m[subID]; ok {
			delete(m, subID)
			if len(m) == 0 {
				delete(b.subs, topic)
			}
			return
		}
	}
}

// Publish доставляет msg всем подписчикам topic (неблокируемо, лишнее отбрасывает).
func (b *Broker) Publish(topic, msg string) {
	b.mu.Lock()
	subs := b.subs[topic]
	b.mu.Unlock()
	for _, s := range subs {
		select {
		case s.ch <- msg:
		default: // канал полон — отбрасываем для этого подписчика
		}
	}
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var b []byte
	for n > 0 {
		b = append([]byte{byte('0' + n%10)}, b...)
		n /= 10
	}
	return string(b)
}
