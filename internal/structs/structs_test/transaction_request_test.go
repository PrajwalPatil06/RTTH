package structs_test

import (
	"RTTH/internal/structs"
	"testing"
)

// TestClientTransactionValidate_TableDriven covers client transaction validation.
func TestClientTransactionValidate_TableDriven(t *testing.T) {
	tests := []struct {
		name    string
		txn     structs.ClientTransaction
		wantErr string
	}{
		{
			name:    "TC_UT_STC_001 valid transaction",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "Valid Data", Timestamp: 12345},
			wantErr: "",
		},
		{
			name:    "TC_UT_STC_002 missing clientID",
			txn:     structs.ClientTransaction{ClientID: 0, Payload: "Valid Data", Timestamp: 12345},
			wantErr: "transaction ID is required",
		},
		{
			name:    "TC_UT_STC_003 empty payload",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "", Timestamp: 12345},
			wantErr: "payload cannot be empty",
		},
		{
			name:    "TC_UT_STC_004 whitespace payload",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "   ", Timestamp: 12345},
			wantErr: "payload cannot be empty",
		},
		{
			name:    "TC_UT_STC_005 missing timestamp",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "Valid Data", Timestamp: 0},
			wantErr: "timestamp is required",
		},
		{
			name:    "TC_UT_STC_006 error order clientID first",
			txn:     structs.ClientTransaction{ClientID: 0, Payload: "", Timestamp: 0},
			wantErr: "transaction ID is required",
		},
		{
			name:    "TC_UT_STC_007 error order payload second",
			txn:     structs.ClientTransaction{ClientID: 1, Payload: "", Timestamp: 0},
			wantErr: "payload cannot be empty",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := tt.txn.Validate()
			if tt.wantErr == "" {
				if err != nil {
					t.Fatalf("expected no error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected error %q, got nil", tt.wantErr)
			}
			if err.Error() != tt.wantErr {
				t.Fatalf("expected error %q, got %q", tt.wantErr, err.Error())
			}
		})
	}
}
