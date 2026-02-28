package structs

type HeartBeat struct {
	LeaderID  int    `json:"leaderid"`
	Heartbeat   rune `json:"heartbeat"`
	Timestamp int64  `json:"timestamp"`
}