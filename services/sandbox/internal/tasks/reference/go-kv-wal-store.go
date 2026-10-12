//go:build ignore

package task

import "encoding/json"

// LogEntry — запись WAL (JSON-строка).
type LogEntry struct {
	Op    string `json:"op"` // "put" | "delete"
	Key   string `json:"key"`
	Value string `json:"value,omitempty"`
}

// KVStore — KV-хранилище с append-only WAL.
type KVStore struct {
	data map[string]string
	log  []string
}

// NewKV создаёт пустое хранилище.
func NewKV() *KVStore {
	return &KVStore{data: map[string]string{}}
}

// Log возвращает текущий WAL (append-only; не мутируйте).
func (s *KVStore) Log() []string { return s.log }

// Put сохраняет value под key и дописывает запись в WAL; возвращает запись.
func (s *KVStore) Put(key, value string) string {
	rec, _ := json.Marshal(LogEntry{Op: "put", Key: key, Value: value})
	s.log = append(s.log, string(rec))
	s.data[key] = value
	return string(rec)
}

// Delete помечает key удалённым (tombstone), дописывает запись в WAL;
// возвращает запись. Повторный Delete существующего/несуществующего key допустим.
func (s *KVStore) Delete(key string) string {
	rec, _ := json.Marshal(LogEntry{Op: "delete", Key: key})
	s.log = append(s.log, string(rec))
	delete(s.data, key)
	return string(rec)
}

// Get возвращает значение key и true либо ("", false), если key отсутствует
// (никогда не был поставлен или удалён).
func (s *KVStore) Get(key string) (string, bool) {
	v, ok := s.data[key]
	return v, ok
}

// Replay восстанавливает состояние из записей WAL: возвращает НОВОЕ хранилище
// (его Log() — пустой) с состоянием после применения всех записей по порядку.
func Replay(log []string) *KVStore {
	s := NewKV()
	for _, line := range log {
		var e LogEntry
		if err := json.Unmarshal([]byte(line), &e); err != nil {
			continue // некорректная запись — пропускается
		}
		switch e.Op {
		case "put":
			s.data[e.Key] = e.Value
		case "delete":
			delete(s.data, e.Key)
		}
	}
	return s
}
