package store

import (
	"RTTH/internal/structs"
	"sync"
)

type LogStore interface {
	Append(t structs.Transaction) error
	GetByID(id int) (structs.Transaction, error)
	GetAll() map[int]structs.Transaction
}

type MemoryStore struct {
	mu   sync.RWMutex
	data map[int]structs.Transaction
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data: make(map[int]structs.Transaction),
	}
}
func (m *MemoryStore) Append(t structs.Transaction) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	m.data[t.ID]=t
	
	return nil
}
func (m *MemoryStore) GetByID(id int) (structs.Transaction, error) {
	return m.data[id], nil
}
func (m *MemoryStore) GetAll() map[int]structs.Transaction {
	return m.data
}