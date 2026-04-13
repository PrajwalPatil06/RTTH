package structs_test

import (
	"RTTH/internal/structs"
	"testing"
)

// TC_0001 — Validate() covers all five branches: valid, missing ID, whitespace
// payload, empty payload, zero timestamp.
func TestClientTransaction_Validate(t *testing.T) {
	tests := []struct {
		name    string
		txn     structs.ClientTransaction
		wantErr string
	}{
		{
			name:    "valid transaction",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "Valid Data", Timestamp: 12345},
			wantErr: "",
		},
		{
			name:    "missing clientID",
			txn:     structs.ClientTransaction{ClientID: 0, Payload: "Valid Data", Timestamp: 12345},
			wantErr: "transaction ID is required",
		},
		{
			name:    "whitespace-only payload",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "   ", Timestamp: 12345},
			wantErr: "payload cannot be empty",
		},
		{
			name:    "empty payload",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "", Timestamp: 12345},
			wantErr: "payload cannot be empty",
		},
		{
			name:    "zero timestamp",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "Valid Data", Timestamp: 0},
			wantErr: "timestamp is required",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.txn.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Errorf("expected no error, got %v", err)
				}
			} else {
				if err == nil {
					t.Errorf("expected error %q, got nil", tt.wantErr)
				} else if err.Error() != tt.wantErr {
					t.Errorf("expected error %q, got %q", tt.wantErr, err.Error())
				}
			}
		})
	}
}

// TC_0013 — Validate() errors are returned in field order:
// clientID → payload → timestamp.
func TestClientTransaction_Validate_ErrorOrder(t *testing.T) {
	all := structs.ClientTransaction{ClientID: 0, Payload: "", Timestamp: 0}
	err := all.Validate()
	if err == nil || err.Error() != "transaction ID is required" {
		t.Errorf("expected clientID error first, got: %v", err)
	}

	noPayload := structs.ClientTransaction{ClientID: 1, Payload: "", Timestamp: 0}
	err2 := noPayload.Validate()
	if err2 == nil || err2.Error() != "payload cannot be empty" {
		t.Errorf("expected payload error second, got: %v", err2)
	}
}

// TC_0014 — GetRequest only carries ClientID; zero value is the empty state.
func TestGetRequest_Fields(t *testing.T) {
	req := structs.GetRequest{ClientID: 42}
	if req.ClientID != 42 {
		t.Errorf("expected ClientID 42, got %d", req.ClientID)
	}

	empty := structs.GetRequest{}
	if empty.ClientID != 0 {
		t.Errorf("expected zero ClientID on empty struct, got %d", empty.ClientID)
	}
}