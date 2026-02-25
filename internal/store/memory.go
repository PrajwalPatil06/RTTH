package store

import (
	"RTTH/internal/domain"
	"sync"
)

type LogStore interface {
	Append(t domain.Transaction) error
	GetByID(id string) (domain.Transaction, error)
	GetAll() []domain.Transaction
}

type MemoryStore struct {
	data []domain.Transaction
	mu sync.RWMutex
}

