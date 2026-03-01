package structs

type VoteReq struct {
	Term int `json:"term"`
	CandidateID  int    `json:"candidateid"`
	Timestamp int64  `json:"timestamp"`
}

type VoteResp struct {
	VoteGranted bool `json:"votegranted"`
}