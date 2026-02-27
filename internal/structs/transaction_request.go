package structs

import (
	"errors"
	"strings"
)

// old name: transactionrequest
type ClientTransaction struct {
	ClientID  int    `json:"clientid"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}

func (t *ClientTransaction) Validate() error {
	if t.ClientID == 0 {
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
