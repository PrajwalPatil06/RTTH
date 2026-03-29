package store

import (
	"RTTH/internal/structs"
	"errors"
	"maps"
	"sync"
)

type LogStore interface {
	Append(t structs.Transaction) error
	GetByID(id int) (structs.Transaction, error)
	GetAll() map[int]structs.Transaction
}

type MemoryStore struct {
	Mu        sync.RWMutex
	data      map[int]structs.Transaction
	nextIndex int
}

func NewMemoryStore() *MemoryStore {
	return &MemoryStore{
		data:      make(map[int]structs.Transaction),
		nextIndex: 1,
	}
}

func (m *MemoryStore) Append(t structs.Transaction) error {
	m.Mu.Lock()
	defer m.Mu.Unlock()
	t.ID = m.nextIndex
	m.nextIndex++
	m.data[t.ID] = t
	return nil
}

func (m *MemoryStore) GetByID(id int) (structs.Transaction, error) {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	t, ok := m.data[id]
	if !ok {
		return structs.Transaction{}, errors.New("transaction not found")
	}
	return t, nil
}

func (m *MemoryStore) GetAll() map[int]structs.Transaction {
	m.Mu.RLock()
	defer m.Mu.RUnlock()
	temp := make(map[int]structs.Transaction, len(m.data))
	maps.Copy(temp, m.data)
	return temp
}