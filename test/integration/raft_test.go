package integration

import (
	"RTTH/internal/domain"
	"RTTH/internal/handlers"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
)

type testCluster struct {
	nodes   []*domain.Node
	servers []*httptest.Server
}

func newCluster(t *testing.T) *testCluster {
	t.Helper()
	gin.SetMode(gin.TestMode)

	c := &testCluster{
		nodes:   make([]*domain.Node, 3),
		servers: make([]*httptest.Server, 3),
	}

	for i := 0; i < 3; i++ {
		node, err := domain.NewNode(i+1, 300, t.TempDir())
		if err != nil {
			t.Fatalf("NewNode %d: %v", i+1, err)
		}
		c.nodes[i] = node
	}

	for i, node := range c.nodes {
		h := handlers.NewHandler(store.NewMemoryStore(), node)
		r := gin.New()
		r.POST("/appendentries", h.HandleAppendEntries)
		r.POST("/requestvote", h.HandleVoteRequest)
		r.POST("/append", h.HandleAppendTransactionReq)
		r.POST("/transfer", h.HandleTransfer)
		r.POST("/balance", h.HandleBalance)
		r.GET("/blockchain", h.HandleGetBlockchain)

		srv := httptest.NewServer(r)
		c.servers[i] = srv
		t.Cleanup(srv.Close)
	}

	for i, node := range c.nodes {
		node.Mu.Lock()
		node.OtherNodes = make(map[int]string)
		for j, srv := range c.servers {
			if j != i {
				node.OtherNodes[j+1] = srv.URL
			}
		}
		node.Mu.Unlock()
	}

	return c
}

func (c *testCluster) forceLeader(idx int) {
	for i, node := range c.nodes {
		node.Mu.Lock()
		if i == idx {
			node.State = "Leader"
			node.CurrentTerm = 1
			node.LeaderId = node.Id
		} else {
			node.State = "Follower"
			node.CurrentTerm = 1
			node.LeaderId = c.nodes[idx].Id
		}
		node.Mu.Unlock()
	}
}

func (c *testCluster) leaderNode() *domain.Node {
	for _, n := range c.nodes {
		n.Mu.Lock()
		st := n.State
		n.Mu.Unlock()
		if st == "Leader" {
			return n
		}
	}
	return nil
}

func doPost(url string, body interface{}) (*http.Response, error) {
	data, err := json.Marshal(body)
	if err != nil {
		return nil, err
	}
	return http.Post(url, "application/json", bytes.NewReader(data))
}

func decodeJSON[T any](t *testing.T, resp *http.Response) T {
	t.Helper()
	var v T
	if err := json.NewDecoder(resp.Body).Decode(&v); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	return v
}

// TestRaft_TC_INT_001_HeartbeatFromLeader covers leader heartbeat propagation.
func TestRaft_TC_INT_001_HeartbeatFromLeader(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	leader := c.nodes[0]
	follower := c.nodes[1]

	leader.Mu.Lock()
	leaderTerm := leader.CurrentTerm
	leaderID := leader.Id
	leader.Mu.Unlock()

	followerURL := c.servers[1].URL
	reqBody := structs.AppendEntriesReq{
		Term:         leaderTerm,
		LeaderID:     leaderID,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []structs.Transaction{},
		LeaderCommit: 0,
	}
	resp, err := doPost(followerURL+"/appendentries", reqBody)
	if err != nil {
		t.Fatalf("POST /appendentries: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	aeResp := decodeJSON[structs.AppendEntriesResp](t, resp)
	if !aeResp.Success {
		t.Error("want success=true for valid heartbeat")
	}

	follower.Mu.Lock()
	lid := follower.LeaderId
	follower.Mu.Unlock()
	if lid != leaderID {
		t.Errorf("follower LeaderId: want %d, got %d", leaderID, lid)
	}
}

// TestRaft_TC_INT_002_LogReplication_SingleEntry covers single-entry replication.
func TestRaft_TC_INT_002_LogReplication_SingleEntry(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	leader := c.nodes[0]
	leader.Mu.Lock()
	leader.Log = append(leader.Log, structs.Transaction{
		ClientID: 1, Payload: "2 50", Term: 1,
	})
	leader.Mu.Unlock()

	followerURL := c.servers[1].URL
	reqBody := structs.AppendEntriesReq{
		Term:     1,
		LeaderID: leader.Id,
		Entries: []structs.Transaction{
			{ClientID: 1, Payload: "2 50", Term: 1},
		},
		LeaderCommit: 0,
	}
	resp, err := doPost(followerURL+"/appendentries", reqBody)
	if err != nil {
		t.Fatalf("POST /appendentries: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	aeResp := decodeJSON[structs.AppendEntriesResp](t, resp)
	if !aeResp.Success {
		t.Error("want success=true after entry replication")
	}

	c.nodes[1].Mu.Lock()
	logLen := len(c.nodes[1].Log)
	c.nodes[1].Mu.Unlock()

	if logLen != 1 {
		t.Errorf("follower log length: want 1, got %d", logLen)
	}
}

// TestRaft_TC_INT_003_VoteRequest_CausesStepDown covers higher-term step-down behavior.
func TestRaft_TC_INT_003_VoteRequest_CausesStepDown(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	leaderURL := c.servers[0].URL
	voteReq := structs.VoteReq{
		Term:         5,
		CandidateID:  2,
		LastLogIndex: 0,
		LastLogTerm:  0,
		Timestamp:    time.Now().UnixMilli(),
	}
	resp, err := doPost(leaderURL+"/requestvote", voteReq)
	if err != nil {
		t.Fatalf("POST /requestvote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	vResp := decodeJSON[structs.VoteResp](t, resp)
	if !vResp.VoteGranted {
		t.Error("want VoteGranted=true when candidate term is higher")
	}

	c.nodes[0].Mu.Lock()
	state := c.nodes[0].State
	c.nodes[0].Mu.Unlock()

	if state != "Follower" {
		t.Errorf("former leader should be Follower after higher-term vote request, got %q", state)
	}
}

// TestRaft_TC_INT_004_CommitPropagation covers commit index propagation.
func TestRaft_TC_INT_004_CommitPropagation(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	entry := structs.Transaction{ClientID: 1, Payload: "2 100", Term: 1}

	for i := 1; i < 3; i++ {
		reqBody := structs.AppendEntriesReq{
			Term:         1,
			LeaderID:     c.nodes[0].Id,
			Entries:      []structs.Transaction{entry},
			LeaderCommit: 0,
		}
		resp, err := doPost(c.servers[i].URL+"/appendentries", reqBody)
		if err != nil {
			t.Fatalf("replicate to node %d: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("node %d: want 200, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	for i := 1; i < 3; i++ {
		reqBody := structs.AppendEntriesReq{
			Term:         1,
			LeaderID:     c.nodes[0].Id,
			Entries:      []structs.Transaction{},
			LeaderCommit: 1,
		}
		resp, err := doPost(c.servers[i].URL+"/appendentries", reqBody)
		if err != nil {
			t.Fatalf("commit heartbeat to node %d: %v", i+1, err)
		}
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("node %d: want 200, got %d", i+1, resp.StatusCode)
		}
		resp.Body.Close()
	}

	for i := 1; i < 3; i++ {
		c.nodes[i].Mu.Lock()
		ci := c.nodes[i].CommitIndex
		la := c.nodes[i].LastApplied
		c.nodes[i].Mu.Unlock()

		if ci != 1 {
			t.Errorf("node %d CommitIndex: want 1, got %d", i+1, ci)
		}
		if la != 1 {
			t.Errorf("node %d LastApplied: want 1, got %d", i+1, la)
		}
	}
}

// TestRaft_TC_INT_005_LogConflict_RetrySucceeds covers conflict retry behavior.
func TestRaft_TC_INT_005_LogConflict_RetrySucceeds(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	followerURL := c.servers[1].URL

	c.nodes[1].Mu.Lock()
	c.nodes[1].Log = append(c.nodes[1].Log, structs.Transaction{
		ClientID: 9, Payload: "stale", Term: 1,
	})
	c.nodes[1].CurrentTerm = 1
	c.nodes[1].Mu.Unlock()

	badReq := structs.AppendEntriesReq{
		Term: 2, LeaderID: c.nodes[0].Id,
		PrevLogIndex: 1, PrevLogTerm: 2,
		Entries:      []structs.Transaction{{ClientID: 1, Payload: "correct", Term: 2}},
		LeaderCommit: 0,
	}
	resp, err := doPost(followerURL+"/appendentries", badReq)
	if err != nil {
		t.Fatalf("POST bad appendentries: %v", err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}
	r1 := decodeJSON[structs.AppendEntriesResp](t, resp)
	resp.Body.Close()
	if r1.Success {
		t.Error("want success=false on PrevLogTerm mismatch")
	}

	goodReq := structs.AppendEntriesReq{
		Term: 2, LeaderID: c.nodes[0].Id,
		PrevLogIndex: 0, PrevLogTerm: 0,
		Entries:      []structs.Transaction{{ClientID: 1, Payload: "correct", Term: 2}},
		LeaderCommit: 0,
	}
	resp2, err := doPost(followerURL+"/appendentries", goodReq)
	if err != nil {
		t.Fatalf("POST good appendentries: %v", err)
	}
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp2.StatusCode)
	}
	r2 := decodeJSON[structs.AppendEntriesResp](t, resp2)
	resp2.Body.Close()
	if !r2.Success {
		t.Error("want success=true after corrected retry")
	}

	c.nodes[1].Mu.Lock()
	logLen := len(c.nodes[1].Log)
	payload := ""
	if logLen > 0 {
		payload = c.nodes[1].Log[0].Payload
	}
	c.nodes[1].Mu.Unlock()

	if logLen != 1 || payload != "correct" {
		t.Errorf("follower log after retry: want 1 entry 'correct', got len=%d payload=%q", logLen, payload)
	}
}

// TestRaft_TC_INT_006_AppendEntries_RejectsStaleTerm covers stale-term rejection.
func TestRaft_TC_INT_006_AppendEntries_RejectsStaleTerm(t *testing.T) {
	c := newCluster(t)

	c.nodes[1].Mu.Lock()
	c.nodes[1].CurrentTerm = 3
	c.nodes[1].LeaderId = 77
	c.nodes[1].Mu.Unlock()

	reqBody := structs.AppendEntriesReq{
		Term:         2,
		LeaderID:     1,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []structs.Transaction{},
		LeaderCommit: 0,
	}
	resp, err := doPost(c.servers[1].URL+"/appendentries", reqBody)
	if err != nil {
		t.Fatalf("POST /appendentries: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	aeResp := decodeJSON[structs.AppendEntriesResp](t, resp)
	if aeResp.Success {
		t.Fatal("want success=false for stale leader term")
	}
	if aeResp.Term != 3 {
		t.Fatalf("response term: want 3, got %d", aeResp.Term)
	}

	c.nodes[1].Mu.Lock()
	term := c.nodes[1].CurrentTerm
	leaderID := c.nodes[1].LeaderId
	c.nodes[1].Mu.Unlock()
	if term != 3 {
		t.Fatalf("follower term changed: want 3, got %d", term)
	}
	if leaderID != 77 {
		t.Fatalf("leader id changed on stale append: want 77, got %d", leaderID)
	}
}

// TestRaft_TC_INT_007_AppendEntries_RejectsMissingPrevLog covers missing previous-log rejection.
func TestRaft_TC_INT_007_AppendEntries_RejectsMissingPrevLog(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	c.nodes[1].Mu.Lock()
	c.nodes[1].Log = append(c.nodes[1].Log, structs.Transaction{ClientID: 9, Payload: "x", Term: 1})
	c.nodes[1].Mu.Unlock()

	reqBody := structs.AppendEntriesReq{
		Term:         1,
		LeaderID:     c.nodes[0].Id,
		PrevLogIndex: 2,
		PrevLogTerm:  1,
		Entries:      []structs.Transaction{{ClientID: 1, Payload: "2 10", Term: 1}},
		LeaderCommit: 0,
	}
	resp, err := doPost(c.servers[1].URL+"/appendentries", reqBody)
	if err != nil {
		t.Fatalf("POST /appendentries: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	aeResp := decodeJSON[structs.AppendEntriesResp](t, resp)
	if aeResp.Success {
		t.Fatal("want success=false when PrevLogIndex exceeds follower log length")
	}

	c.nodes[1].Mu.Lock()
	logLen := len(c.nodes[1].Log)
	c.nodes[1].Mu.Unlock()
	if logLen != 1 {
		t.Fatalf("log should be unchanged on missing prev log index: want 1, got %d", logLen)
	}
}

// TestRaft_TC_INT_008_LeaderCommit_CappedByFollowerLogLength covers leader-commit capping.
func TestRaft_TC_INT_008_LeaderCommit_CappedByFollowerLogLength(t *testing.T) {
	c := newCluster(t)
	c.forceLeader(0)

	reqBody := structs.AppendEntriesReq{
		Term:         1,
		LeaderID:     c.nodes[0].Id,
		PrevLogIndex: 0,
		PrevLogTerm:  0,
		Entries:      []structs.Transaction{},
		LeaderCommit: 5,
	}
	resp, err := doPost(c.servers[1].URL+"/appendentries", reqBody)
	if err != nil {
		t.Fatalf("POST /appendentries: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	aeResp := decodeJSON[structs.AppendEntriesResp](t, resp)
	if !aeResp.Success {
		t.Fatal("want success=true for heartbeat")
	}

	c.nodes[1].Mu.Lock()
	ci := c.nodes[1].CommitIndex
	la := c.nodes[1].LastApplied
	c.nodes[1].Mu.Unlock()
	if ci != 0 || la != 0 {
		t.Fatalf("commit/apply should be capped by local log length: want (0,0), got (%d,%d)", ci, la)
	}
}

// TestRaft_TC_INT_009_VoteRequest_OneVotePerTerm covers one-vote-per-term behavior.
func TestRaft_TC_INT_009_VoteRequest_OneVotePerTerm(t *testing.T) {
	c := newCluster(t)

	first := structs.VoteReq{
		Term:         4,
		CandidateID:  2,
		LastLogIndex: 0,
		LastLogTerm:  0,
		Timestamp:    time.Now().UnixMilli(),
	}
	resp1, err := doPost(c.servers[0].URL+"/requestvote", first)
	if err != nil {
		t.Fatalf("POST first /requestvote: %v", err)
	}
	defer resp1.Body.Close()
	if resp1.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp1.StatusCode)
	}
	v1 := decodeJSON[structs.VoteResp](t, resp1)
	if !v1.VoteGranted {
		t.Fatal("expected first vote in term to be granted")
	}

	second := structs.VoteReq{
		Term:         4,
		CandidateID:  3,
		LastLogIndex: 0,
		LastLogTerm:  0,
		Timestamp:    time.Now().UnixMilli(),
	}
	resp2, err := doPost(c.servers[0].URL+"/requestvote", second)
	if err != nil {
		t.Fatalf("POST second /requestvote: %v", err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp2.StatusCode)
	}
	v2 := decodeJSON[structs.VoteResp](t, resp2)
	if v2.VoteGranted {
		t.Fatal("expected second vote in same term for different candidate to be rejected")
	}
}

// TestRaft_TC_INT_010_VoteRequest_RejectsOutdatedLog covers outdated-log vote rejection.
func TestRaft_TC_INT_010_VoteRequest_RejectsOutdatedLog(t *testing.T) {
	c := newCluster(t)

	c.nodes[0].Mu.Lock()
	c.nodes[0].CurrentTerm = 3
	c.nodes[0].Log = append(c.nodes[0].Log, structs.Transaction{ClientID: 1, Payload: "2 1", Term: 3})
	c.nodes[0].Mu.Unlock()

	req := structs.VoteReq{
		Term:         3,
		CandidateID:  2,
		LastLogIndex: 10,
		LastLogTerm:  2,
		Timestamp:    time.Now().UnixMilli(),
	}
	resp, err := doPost(c.servers[0].URL+"/requestvote", req)
	if err != nil {
		t.Fatalf("POST /requestvote: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("want 200, got %d", resp.StatusCode)
	}

	v := decodeJSON[structs.VoteResp](t, resp)
	if v.VoteGranted {
		t.Fatal("expected vote to be rejected for candidate with stale last log term")
	}
	if v.Term != 3 {
		t.Fatalf("response term: want 3, got %d", v.Term)
	}
}

// TestRaft_TC_INT_011_Election_OneLeaderElected covers single-leader election behavior.
func TestRaft_TC_INT_011_Election_OneLeaderElected(t *testing.T) {
	c := newCluster(t)

	c.nodes[0].Mu.Lock()
	c.nodes[0].CurrentTerm = 0
	c.nodes[0].Mu.Unlock()

	done := make(chan struct{})
	go func() {
		c.nodes[0].StartElection()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("election did not complete within 2 s")
	}

	leaders := 0
	for _, n := range c.nodes {
		n.Mu.Lock()
		if n.State == "Leader" {
			leaders++
		}
		n.Mu.Unlock()
	}
	if leaders != 1 {
		t.Errorf("want exactly 1 leader after election, got %d", leaders)
	}

	terms := make([]int, 3)
	for i, n := range c.nodes {
		n.Mu.Lock()
		terms[i] = n.CurrentTerm
		n.Mu.Unlock()
	}
	for i := 1; i < 3; i++ {
		if terms[i] != terms[0] {
			t.Errorf("term mismatch: node 1 term=%d, node %d term=%d", terms[0], i+1, terms[i])
		}
	}
}
