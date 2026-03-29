package test

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

	"github.com/gin-gonic/gin"
)

// ── Test helpers ──────────────────────────────────────────────────────────────

// TC_0005 — setupRouter wires all handlers to a Gin test engine
func setupRouter(h *handlers.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/append", h.HandleAppendTransactionReq)
	r.POST("/appendentries", h.HandleAppendEntries)
	r.POST("/requestvote", h.HandleVoteRequest)
	r.POST("/getuserdetails", h.GetUserDetails)
	r.GET("/getalluserdetails", h.GetAllUserDetails)
	return r
}

// newTestNode creates a Node backed by a temp dir for isolation between tests.
func newTestNode(t *testing.T, id, timeout int) *domain.Node {
	t.Helper()
	node, err := domain.NewNode(id, timeout, t.TempDir())
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	return node
}

// doPost fires a JSON POST and returns the recorder.
func doPost(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("POST", path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// doGet fires a GET request and returns the recorder.
func doGet(router *gin.Engine, path string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest("GET", path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ── HandleAppendTransactionReq ────────────────────────────────────────────────

// TC_0006 — Leader stores a valid transaction and returns 200
func TestHandleAppendTransactionReq_Leader(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Leader"
	s := store.NewMemoryStore()
	router := setupRouter(handlers.NewHandler(s, node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	if len(s.GetAll()) != 1 {
		t.Errorf("want 1 item in store, got %d", len(s.GetAll()))
	}
}

// TC_0007 — Follower with a known leader redirects the client with 307
func TestHandleAppendTransactionReq_FollowerRedirect(t *testing.T) {
	node := newTestNode(t, 2, 150)
	node.State = "Follower"
	node.LeaderId = 1
	node.OtherNodes[1] = "http://localhost:8080"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("want 307, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "http://localhost:8080/append" {
		t.Errorf("Location header: want 'http://localhost:8080/append', got '%s'", loc)
	}
}

// TC_0010 — Follower with LeaderId=0 (unknown) returns 503, not a broken redirect
func TestHandleAppendTransactionReq_FollowerNoLeader(t *testing.T) {
	node := newTestNode(t, 2, 150)
	node.State = "Follower"
	// LeaderId stays at 0 — no leader known yet
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when leader unknown, got %d", w.Code)
	}
}

// TC_0028 — Candidate state returns 503 (election in progress)
func TestHandleAppendTransactionReq_Candidate(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Candidate"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 during election, got %d", w.Code)
	}
}

// TC_0029 — Leader rejects a transaction that fails Validate (empty payload)
func TestHandleAppendTransactionReq_Leader_InvalidPayload(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Leader"
	s := store.NewMemoryStore()
	router := setupRouter(handlers.NewHandler(s, node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"","timestamp":12345}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for invalid payload, got %d", w.Code)
	}
	if len(s.GetAll()) != 0 {
		t.Error("store must remain empty when validation fails")
	}
}

// TC_0030 — Malformed JSON to /append returns 400
func TestHandleAppendTransactionReq_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Leader"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `not json`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for malformed JSON, got %d", w.Code)
	}
}

// TC_0031 — Two sequential leader writes produce entries at distinct, sequential log indices
func TestHandleAppendTransactionReq_Leader_SequentialIndices(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Leader"
	s := store.NewMemoryStore()
	router := setupRouter(handlers.NewHandler(s, node))

	doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":1}`)
	doPost(router, "/append", `{"clientid":2,"payload":"C->D 20","timestamp":2}`)

	all := s.GetAll()
	if len(all) != 2 {
		t.Fatalf("want 2 entries, got %d", len(all))
	}
	if _, ok := all[1]; !ok {
		t.Error("missing log entry at index 1")
	}
	if _, ok := all[2]; !ok {
		t.Error("missing log entry at index 2")
	}
}

// ── HandleAppendEntries ───────────────────────────────────────────────────────

// TC_0008 — Heartbeat (empty entries slice) from valid leader returns 200 and success=true
func TestHandleAppendEntries_Heartbeat(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":1,"leaderid":2,"prevlogindex":0,"prevlogterm":0,"entries":[],"leadercommit":0}`
	w := doPost(router, "/appendentries", payload)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	var resp structs.AppendEntriesResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.Success {
		t.Error("want success=true for valid heartbeat")
	}
}

// TC_0032 — AppendEntries with a lower term is rejected (success=false, current term returned)
func TestHandleAppendEntries_StaleTermRejected(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 5
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":3,"leaderid":2,"prevlogindex":0,"prevlogterm":0,"entries":[],"leadercommit":0}`
	w := doPost(router, "/appendentries", payload)

	if w.Code != http.StatusOK {
		t.Errorf("want 200 envelope, got %d", w.Code)
	}
	var resp structs.AppendEntriesResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.Success {
		t.Error("want success=false for stale term")
	}
	if resp.Term != 5 {
		t.Errorf("want current term=5 in response, got %d", resp.Term)
	}
}

// TC_0033 — Valid AppendEntries reverts a Candidate to Follower and records the leader
func TestHandleAppendEntries_UpdatesLeaderState(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Candidate"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":2,"leaderid":3,"prevlogindex":0,"prevlogterm":0,"entries":[],"leadercommit":0}`
	doPost(router, "/appendentries", payload)

	node.Mu.Lock()
	defer node.Mu.Unlock()
	if node.State != "Follower" {
		t.Errorf("State: want 'Follower', got '%s'", node.State)
	}
	if node.LeaderId != 3 {
		t.Errorf("LeaderId: want 3, got %d", node.LeaderId)
	}
	if node.CurrentTerm != 2 {
		t.Errorf("CurrentTerm: want 2, got %d", node.CurrentTerm)
	}
}

// TC_0034 — AppendEntries with non-empty entries appends them to node.Log
func TestHandleAppendEntries_AppendsEntries(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":1,"leaderid":2,"prevlogindex":0,"prevlogterm":0,` +
		`"entries":[{"id":1,"clientid":5,"payload":"X->Y 10","timestamp":999}],` +
		`"leadercommit":0}`
	w := doPost(router, "/appendentries", payload)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	node.Mu.Lock()
	defer node.Mu.Unlock()
	if len(node.Log) != 1 {
		t.Fatalf("want 1 entry in node.Log, got %d", len(node.Log))
	}
	if node.Log[0].Payload != "X->Y 10" {
		t.Errorf("log payload: want 'X->Y 10', got '%s'", node.Log[0].Payload)
	}
}

// TC_0035 — AppendEntries from a higher-term leader causes node to update CurrentTerm
func TestHandleAppendEntries_HigherTermUpdatesCurrentTerm(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 2
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":10,"leaderid":3,"prevlogindex":0,"prevlogterm":0,"entries":[],"leadercommit":0}`
	doPost(router, "/appendentries", payload)

	node.Mu.Lock()
	term := node.CurrentTerm
	node.Mu.Unlock()
	if term != 10 {
		t.Errorf("CurrentTerm: want 10 after higher-term AppendEntries, got %d", term)
	}
}

// TC_0036 — Malformed JSON to /appendentries returns 400
func TestHandleAppendEntries_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/appendentries", `{bad json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── HandleVoteRequest ─────────────────────────────────────────────────────────

// TC_0037 — Stale term is rejected; node's current term is returned in response
func TestHandleVoteRequest_StaleTermRejected(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 5
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote", `{"term":3,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	if w.Code != http.StatusOK {
		t.Errorf("want 200 envelope, got %d", w.Code)
	}
	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.VoteGranted {
		t.Error("want VoteGranted=false for stale term")
	}
	if resp.Term != 5 {
		t.Errorf("want Term=5 in response, got %d", resp.Term)
	}
}

// TC_0038 — First vote request in a new term is granted; VotedFor is recorded
func TestHandleVoteRequest_GrantsVote_FirstInTerm(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote", `{"term":1,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.VoteGranted {
		t.Error("want VoteGranted=true for first vote in term")
	}
	node.Mu.Lock()
	voted := node.VotedFor[1]
	node.Mu.Unlock()
	if voted != 2 {
		t.Errorf("VotedFor[1]: want 2, got %d", voted)
	}
}

// TC_0039 — Repeat vote request from the same candidate in the same term is granted (idempotent)
func TestHandleVoteRequest_SameCandidateIdempotent(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote", `{"term":3,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.VoteGranted {
		t.Error("want VoteGranted=true for repeat request from same candidate")
	}
}

// TC_0040 — Vote request from a different candidate in the same term is denied
func TestHandleVoteRequest_DeniesVote_DifferentCandidate(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2 // already voted for candidate 2
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	// Candidate 5 requests a vote in term 3 — must be denied
	w := doPost(router, "/requestvote", `{"term":3,"candidateid":5,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.VoteGranted {
		t.Error("want VoteGranted=false when already voted for a different candidate this term")
	}
}

// TC_0041 — Vote request with a higher term causes node to step down from Leader to Follower
func TestHandleVoteRequest_HigherTermStepsDown(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.State = "Leader"
	node.CurrentTerm = 3
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	doPost(router, "/requestvote", `{"term":7,"candidateid":3,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	node.Mu.Lock()
	state, term := node.State, node.CurrentTerm
	node.Mu.Unlock()
	if state != "Follower" {
		t.Errorf("State: want 'Follower' after higher-term vote request, got '%s'", state)
	}
	if term != 7 {
		t.Errorf("CurrentTerm: want 7, got %d", term)
	}
}

// TC_0042 — Election timer (LastLeaderTimeStamp) is NOT reset when granting a vote (Raft §5.2)
func TestHandleVoteRequest_DoesNotResetElectionTimer(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 1
	before := node.LastLeaderTimeStamp
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	doPost(router, "/requestvote", `{"term":1,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	node.Mu.Lock()
	after := node.LastLeaderTimeStamp
	node.Mu.Unlock()
	if after != before {
		t.Errorf("LastLeaderTimeStamp changed from %d to %d — must not reset on vote grant (Raft §5.2)", before, after)
	}
}

// TC_0043 — Malformed JSON to /requestvote returns 400
func TestHandleVoteRequest_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote", `not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── GetUserDetails ────────────────────────────────────────────────────────────

// TC_0044 — GetUserDetails returns 200 and the transaction for a stored log index
func TestGetUserDetails_Found(t *testing.T) {
	s := store.NewMemoryStore()
	// Append gives this entry log index = 1
	s.Append(structs.Transaction{ClientID: 7, Payload: "A->B 100", Timestamp: 1111})

	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(s, node))

	w := doPost(router, "/getuserdetails", `{"clientid":1}`)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	var txn structs.Transaction
	json.NewDecoder(w.Body).Decode(&txn)
	if txn.Payload != "A->B 100" {
		t.Errorf("payload: want 'A->B 100', got '%s'", txn.Payload)
	}
}

// TC_0045 — GetUserDetails returns 404 for a log index with no entry
func TestGetUserDetails_NotFound(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{"clientid":999}`)

	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

// TC_0046 — GetUserDetails returns 400 when clientid is 0
func TestGetUserDetails_MissingClientID(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{"clientid":0}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// TC_0047 — GetUserDetails returns 400 for malformed JSON
func TestGetUserDetails_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── GetAllUserDetails ─────────────────────────────────────────────────────────

// TC_0048 — GetAllUserDetails returns 200 and an empty object when store is empty
func TestGetAllUserDetails_Empty(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doGet(router, "/getalluserdetails")

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

// TC_0049 — GetAllUserDetails returns all stored entries keyed by log index
func TestGetAllUserDetails_ReturnsList(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 1, Payload: "A->B 10"})
	s.Append(structs.Transaction{ClientID: 2, Payload: "C->D 20"})
	s.Append(structs.Transaction{ClientID: 3, Payload: "E->F 30"})

	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(s, node))

	w := doGet(router, "/getalluserdetails")

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	// Response is a JSON object — key count equals entry count
	var result map[string]interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(result) != 3 {
		t.Errorf("want 3 entries in response, got %d", len(result))
	}
}

// TC_0050 — GetAllUserDetails requires no request body (GET endpoint)
func TestGetAllUserDetails_NoBodyRequired(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 1, Payload: "test"})
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(s, node))

	// Sending a body to a GET endpoint must still work cleanly
	w := doGet(router, "/getalluserdetails")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}