package structs

type TransactionRequest struct {
	ID        int `json:"id"` //id of client
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
}