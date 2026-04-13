package structs

// Transaction is a single log entry in both the RAFT log and the blockchain.
type Transaction struct {
	ID        int    `json:"id"`
	ClientID  int    `json:"clientid"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
	Term      int    `json:"term"` // RAFT term when this entry was created
}