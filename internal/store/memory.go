package store

import (
	"RTTH/internal/structs"
	"sync"
)

type LogStore interface {
	Append(t structs.Transaction) error
	GetByID(id string) (structs.Transaction, error)
	GetAll() []structs.Transaction
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
func (m *MemoryStore) GetByID(id string) (structs.Transaction, error) {
	return structs.Transaction{},nil
}
func (m *MemoryStore) GetAll() []structs.Transaction {
	return []structs.Transaction{}
}