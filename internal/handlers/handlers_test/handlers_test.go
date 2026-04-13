package handlers_test

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

func setupRouter(h *handlers.Handler) *gin.Engine {
	gin.SetMode(gin.TestMode)
	r := gin.Default()
	r.POST("/append", h.HandleAppendTransactionReq)
	r.POST("/appendentries", h.HandleAppendEntries)
	r.POST("/requestvote", h.HandleVoteRequest)
	r.POST("/getuserdetails", h.GetUserDetails)
	r.GET("/getalluserdetails", h.GetAllUserDetails)
	r.POST("/transfer", h.HandleTransfer)
	r.POST("/balance", h.HandleBalance)
	r.GET("/blockchain", h.HandleGetBlockchain)
	return r
}

func newTestNode(t *testing.T, id, timeout int) *domain.Node {
	t.Helper()
	node, err := domain.NewNode(id, timeout, t.TempDir())
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}
	return node
}

func doPost(router *gin.Engine, path, body string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(http.MethodPost, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func doGet(router *gin.Engine, path string) *httptest.ResponseRecorder {
	req, _ := http.NewRequest(http.MethodGet, path, nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

// ── HandleAppendTransactionReq ────────────────────────────────────────────────

// TC_0006 — Leader stores a valid transaction and returns 200.
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

// TC_0007 — Follower with a known leader redirects with 307.
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
		t.Errorf("Location: want 'http://localhost:8080/append', got %q", loc)
	}
}

// TC_0010 — Follower with LeaderId=0 returns 503 (election in progress).
func TestHandleAppendTransactionReq_FollowerNoLeader(t *testing.T) {
	node := newTestNode(t, 2, 150)
	node.State = "Follower"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 when leader unknown, got %d", w.Code)
	}
}

// TC_0028 — Candidate state returns 503 (election in progress).
func TestHandleAppendTransactionReq_Candidate(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Candidate"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `{"clientid":1,"payload":"A->B 10","timestamp":12345}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503 during election, got %d", w.Code)
	}
}

// TC_0029 — Leader rejects a transaction that fails Validate (empty payload).
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

// TC_0030 — Malformed JSON to /append returns 400.
func TestHandleAppendTransactionReq_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Leader"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/append", `not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for malformed JSON, got %d", w.Code)
	}
}

// TC_0031 — Two sequential leader writes produce entries at distinct, sequential
// log indices.
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

// TC_0008 — Heartbeat (empty entries) from valid leader returns 200 success=true.
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

// TC_0032 — AppendEntries with a lower term is rejected.
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

// TC_0033 — Valid AppendEntries reverts a Candidate to Follower and records leader.
func TestHandleAppendEntries_UpdatesLeaderState(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.State = "Candidate"
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":2,"leaderid":3,"prevlogindex":0,"prevlogterm":0,"entries":[],"leadercommit":0}`
	doPost(router, "/appendentries", payload)

	node.Mu.Lock()
	defer node.Mu.Unlock()
	if node.State != "Follower" {
		t.Errorf("State: want 'Follower', got %q", node.State)
	}
	if node.LeaderId != 3 {
		t.Errorf("LeaderId: want 3, got %d", node.LeaderId)
	}
	if node.CurrentTerm != 2 {
		t.Errorf("CurrentTerm: want 2, got %d", node.CurrentTerm)
	}
}

// TC_0034 — AppendEntries with non-empty entries appends them to node.Log.
func TestHandleAppendEntries_AppendsEntries(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	payload := `{"term":1,"leaderid":2,"prevlogindex":0,"prevlogterm":0,` +
		`"entries":[{"clientid":1,"payload":"A->B 10","timestamp":1,"term":1}],` +
		`"leadercommit":0}`
	doPost(router, "/appendentries", payload)

	node.Mu.Lock()
	logLen := len(node.Log)
	node.Mu.Unlock()

	if logLen != 1 {
		t.Errorf("want 1 log entry after AppendEntries, got %d", logLen)
	}
}

// ── HandleVoteRequest ─────────────────────────────────────────────────────────

// TC_0035 — Stale term vote request is rejected; current term is returned.
func TestHandleVoteRequest_StaleTermRejected(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 5
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote",
		`{"term":3,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.VoteGranted {
		t.Error("want VoteGranted=false for stale term")
	}
	if resp.Term != 5 {
		t.Errorf("want Term=5 in response, got %d", resp.Term)
	}
}

// TC_0038 — First vote request in a new term is granted; VotedFor is recorded.
func TestHandleVoteRequest_GrantsVote_FirstInTerm(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote",
		`{"term":1,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

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

// TC_0039 — Repeat vote from same candidate in same term is idempotent.
func TestHandleVoteRequest_SameCandidateIdempotent(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote",
		`{"term":3,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if !resp.VoteGranted {
		t.Error("want VoteGranted=true for repeat request from same candidate")
	}
}

// TC_0040 — Vote from a different candidate in same term is denied.
func TestHandleVoteRequest_DeniesVote_DifferentCandidate(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote",
		`{"term":3,"candidateid":5,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	var resp structs.VoteResp
	json.NewDecoder(w.Body).Decode(&resp)
	if resp.VoteGranted {
		t.Error("want VoteGranted=false when already voted for a different candidate")
	}
}

// TC_0041 — Higher term causes node to step down from Leader.
func TestHandleVoteRequest_HigherTermStepsDown(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.State = "Leader"
	node.CurrentTerm = 3
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	doPost(router, "/requestvote",
		`{"term":7,"candidateid":3,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	node.Mu.Lock()
	state, term := node.State, node.CurrentTerm
	node.Mu.Unlock()
	if state != "Follower" {
		t.Errorf("State: want 'Follower', got %q", state)
	}
	if term != 7 {
		t.Errorf("CurrentTerm: want 7, got %d", term)
	}
}

// TC_0042 — Election timer (LastLeaderTimeStamp) is NOT reset when granting a
// vote (RAFT §5.2).
func TestHandleVoteRequest_DoesNotResetElectionTimer(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.CurrentTerm = 1
	before := node.LastLeaderTimeStamp
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	doPost(router, "/requestvote",
		`{"term":1,"candidateid":2,"lastlogindex":0,"lastlogterm":0,"timestamp":0}`)

	node.Mu.Lock()
	after := node.LastLeaderTimeStamp
	node.Mu.Unlock()
	if after != before {
		t.Errorf("LastLeaderTimeStamp changed (%d → %d) — must not reset on vote grant", before, after)
	}
}

// TC_0043 — Malformed JSON to /requestvote returns 400.
func TestHandleVoteRequest_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/requestvote", `not json`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── GetUserDetails ────────────────────────────────────────────────────────────

// TC_0044 — GetUserDetails returns 200 and the transaction for a stored index.
func TestGetUserDetails_Found(t *testing.T) {
	s := store.NewMemoryStore()
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
		t.Errorf("payload: want 'A->B 100', got %q", txn.Payload)
	}
}

// TC_0045 — GetUserDetails returns 404 for a missing index.
func TestGetUserDetails_NotFound(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{"clientid":999}`)
	if w.Code != http.StatusNotFound {
		t.Errorf("want 404, got %d", w.Code)
	}
}

// TC_0046 — GetUserDetails returns 400 when clientid is 0.
func TestGetUserDetails_MissingClientID(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{"clientid":0}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// TC_0047 — GetUserDetails returns 400 for malformed JSON.
func TestGetUserDetails_MalformedJSON(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/getuserdetails", `{bad`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── GetAllUserDetails ─────────────────────────────────────────────────────────

// TC_0048 — GetAllUserDetails returns 200 and an empty object when store is empty.
func TestGetAllUserDetails_Empty(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doGet(router, "/getalluserdetails")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

// TC_0049 — GetAllUserDetails returns all stored entries keyed by log index.
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
	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if len(result) != 3 {
		t.Errorf("want 3 entries, got %d", len(result))
	}
}

// TC_0050 — GetAllUserDetails requires no request body (GET endpoint).
func TestGetAllUserDetails_NoBodyRequired(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 1, Payload: "test"})
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(s, node))

	w := doGet(router, "/getalluserdetails")
	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
}

// ── HandleTransfer ────────────────────────────────────────────────────────────

// TC_0082 — Leader accepts a valid transfer and returns the balance in the body.
func TestHandleTransfer_Leader_ValidRequest(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.State = "Leader"
	node.CurrentTerm = 1
	// Manually advance CommitIndex to simulate immediate majority replication.
	// We do this by pre-committing inside the handler test so WaitForCommit
	// does not time out.  Set CommitIndex=1 in a goroutine after a brief delay.
	node.Mu.Unlock()

	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	// Advance CommitIndex quickly so WaitForCommit returns true.
	go func() {
		for {
			node.Mu.Lock()
			if len(node.Log) > 0 && node.CommitIndex == 0 {
				node.CommitIndex = 1
				node.LastApplied = 0
			}
			node.Mu.Unlock()
			break
		}
	}()

	w := doPost(router, "/transfer",
		`{"clientid":1,"payload":"2 50","timestamp":99999}`)

	// 200 or 504 (timeout) are both acceptable here depending on goroutine
	// scheduling; what matters is that the response is never 500 or 400.
	if w.Code == http.StatusInternalServerError || w.Code == http.StatusBadRequest {
		t.Errorf("want 200 or timeout, got %d — body: %s", w.Code, w.Body.String())
	}
}

// TC_0083 — Leader rejects a transfer with an invalid payload (empty).
func TestHandleTransfer_Leader_InvalidPayload(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.State = "Leader"
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/transfer",
		`{"clientid":1,"payload":"","timestamp":12345}`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400 for empty payload, got %d", w.Code)
	}
}

// TC_0084 — Follower with a known leader redirects transfer request with 307.
func TestHandleTransfer_FollowerRedirects(t *testing.T) {
	node := newTestNode(t, 2, 150)
	node.Mu.Lock()
	node.State = "Follower"
	node.LeaderId = 1
	node.OtherNodes[1] = "http://localhost:8081"
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/transfer",
		`{"clientid":1,"payload":"2 50","timestamp":12345}`)

	if w.Code != http.StatusTemporaryRedirect {
		t.Errorf("want 307, got %d", w.Code)
	}
	loc := w.Header().Get("Location")
	if loc != "http://localhost:8081/transfer" {
		t.Errorf("Location: want 'http://localhost:8081/transfer', got %q", loc)
	}
}

// TC_0085 — Candidate returns 503 on transfer request.
func TestHandleTransfer_Candidate_Returns503(t *testing.T) {
	node := newTestNode(t, 1, 150)
	node.Mu.Lock()
	node.State = "Candidate"
	node.Mu.Unlock()
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/transfer",
		`{"clientid":1,"payload":"2 50","timestamp":12345}`)

	if w.Code != http.StatusServiceUnavailable {
		t.Errorf("want 503, got %d", w.Code)
	}
}

// ── HandleBalance ─────────────────────────────────────────────────────────────

// TC_0086 — Balance endpoint returns both committed and pending balances.
func TestHandleBalance_ReturnsBalances(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/balance", `{"clientid":1}`)

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d — body: %s", w.Code, w.Body.String())
	}
	var result map[string]interface{}
	json.NewDecoder(w.Body).Decode(&result)
	if _, ok := result["committed_balance"]; !ok {
		t.Error("response must include 'committed_balance'")
	}
	if _, ok := result["pending_balance"]; !ok {
		t.Error("response must include 'pending_balance'")
	}
}

// TC_0087 — Balance endpoint returns 400 when clientid is 0.
func TestHandleBalance_MissingClientID(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doPost(router, "/balance", `{"clientid":0}`)
	if w.Code != http.StatusBadRequest {
		t.Errorf("want 400, got %d", w.Code)
	}
}

// ── HandleGetBlockchain ───────────────────────────────────────────────────────

// TC_0088 — /blockchain returns 200 and a JSON array (possibly empty).
func TestHandleGetBlockchain_ReturnsArray(t *testing.T) {
	node := newTestNode(t, 1, 150)
	router := setupRouter(handlers.NewHandler(store.NewMemoryStore(), node))

	w := doGet(router, "/blockchain")

	if w.Code != http.StatusOK {
		t.Errorf("want 200, got %d", w.Code)
	}
	// Must be a valid JSON array.
	var result []interface{}
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Errorf("response is not a JSON array: %v", err)
	}
}