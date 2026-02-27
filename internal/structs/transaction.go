package structs

type Transaction struct {
	ID        int `json:"id"`
	Payload   string `json:"payload"`
	Timestamp int64  `json:"timestamp"`
	HeartBeat rune `json:"heartbeat"`
}