package structs

import (
	"errors"
	"strings"
)

type Transaction struct {
	ID        int `json:"id"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

func (t *Transaction) Validate() error {
	if t.ID == 0 {
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
