package domain

import (
	"errors"
	"strings"
)

type Transaction struct {
	ID        string `json:"id"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

func (t *Transaction) Validate() error {
	if strings.TrimSpace(t.ID) == "" {
		return errors.New("transaction ID is required")
	}
	if strings.TrimSpace(t.Payload) == "" {
		return errors.New("payload cannot be empty")
	}
	if t.Timestamp == 0 {
		return errors.New("Timestamp is required")
	}
	return nil
}