package domain_test

import (
	"RTTH/internal/domain"
	"RTTH/internal/persist"
	"RTTH/internal/structs"
	"os"
	"testing"
	"time"
)

// ── NewNode lifecycle ─────────────────────────────────────────────────────────

// TC_0009 — NewNode sets correct initial volatile state; timeout is randomized
// in [base, 2*base).
func TestNewNode_Initialization(t *testing.T) {
	node, err := domain.NewNode(1, 150, t.TempDir())
	if err != nil {
		t.Fatalf("NewNode: unexpected error: %v", err)
	}

	if node.Id != 1 {
		t.Errorf("Id: want 1, got %d", node.Id)
	}
	if node.State != "Follower" {
		t.Errorf("State: want 'Follower', got %q", node.State)
	}
	if node.Timeout < 150 || node.Timeout >= 300 {
		t.Errorf("Timeout: want in [150, 300), got %d", node.Timeout)
	}
	if node.CurrentTerm != 0 {
		t.Errorf("CurrentTerm: want 0 on first boot, got %d", node.CurrentTerm)
	}
	if node.LeaderId != 0 {
		t.Errorf("LeaderId: want 0 (unknown) on startup, got %d", node.LeaderId)
	}
	if node.CommitIndex != 0 {
		t.Errorf("CommitIndex: want 0, got %d", node.CommitIndex)
	}
	if node.LastApplied != 0 {
		t.Errorf("LastApplied: want 0, got %d", node.LastApplied)
	}
	if len(node.OtherNodes) != 3 {
		t.Errorf("OtherNodes: want 3 before Run(), got %d", len(node.OtherNodes))
	}
	if node.VotedFor == nil {
		t.Error("VotedFor: must not be nil")
	}
	if node.Log == nil {
		t.Error("Log: must not be nil")
	}
	if node.Blockchain == nil {
		t.Error("Blockchain: must not be nil")
	}
}

// TC_0025 — NewNode restores CurrentTerm, VotedFor, and Log from a prior
// state file; volatile fields restart clean.
func TestNewNode_LoadsPersistedState(t *testing.T) {
	dir := t.TempDir()

	s, err := persist.NewStorage(dir, 1)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if err := s.Save(persist.State{
		CurrentTerm: 4,
		VotedFor:    map[int]int{4: 2},
		Log: []structs.Transaction{
			{ID: 1, ClientID: 10, Payload: "A->B 50", Timestamp: 1000, Term: 4},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	node, err := domain.NewNode(1, 150, dir)
	if err != nil {
		t.Fatalf("NewNode after simulated crash: %v", err)
	}

	if node.CurrentTerm != 4 {
		t.Errorf("CurrentTerm: want 4, got %d", node.CurrentTerm)
	}
	if node.VotedFor[4] != 2 {
		t.Errorf("VotedFor[4]: want 2, got %d", node.VotedFor[4])
	}
	if len(node.Log) != 1 || node.Log[0].Payload != "A->B 50" {
		t.Errorf("Log: unexpected value %+v", node.Log)
	}
	if node.State != "Follower" {
		t.Errorf("State: must be 'Follower' after recovery, got %q", node.State)
	}
	if node.CommitIndex != 0 {
		t.Errorf("CommitIndex: must be 0 after recovery, got %d", node.CommitIndex)
	}
	if node.LeaderId != 0 {
		t.Errorf("LeaderId: must be 0 (unknown) after recovery, got %d", node.LeaderId)
	}
}

// TC_0026 — Persist() writes all required fields; a fresh Storage.Load confirms.
func TestNode_Persist_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	node, err := domain.NewNode(1, 150, dir)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}

	node.Mu.Lock()
	node.CurrentTerm = 9
	node.VotedFor[9] = 3
	node.Log = append(node.Log, structs.Transaction{
		ID: 1, ClientID: 7, Payload: "X->Y 10", Timestamp: 42, Term: 9,
	})
	if err := node.Persist(); err != nil {
		node.Mu.Unlock()
		t.Fatalf("Persist: %v", err)
	}
	node.Mu.Unlock()

	s, _ := persist.NewStorage(dir, 1)
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Persist: %v", err)
	}

	if loaded.CurrentTerm != 9 {
		t.Errorf("CurrentTerm: want 9, got %d", loaded.CurrentTerm)
	}
	if loaded.VotedFor[9] != 3 {
		t.Errorf("VotedFor[9]: want 3, got %d", loaded.VotedFor[9])
	}
	if len(loaded.Log) != 1 || loaded.Log[0].Payload != "X->Y 10" {
		t.Errorf("Log: unexpected value %+v", loaded.Log)
	}
}

// TC_0027 — NewNode returns an error if the dataDir path cannot be created.
func TestNewNode_BadDataDir(t *testing.T) {
	blockingFile := t.TempDir() + "/notadir"
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := domain.NewNode(1, 150, blockingFile+"/subdir")
	if err == nil {
		t.Error("expected error when dataDir path is blocked by a file, got nil")
	}
}

// ── ProcessVoteRequest ────────────────────────────────────────────────────────

// TC_0051 — Stale term in vote request is rejected; current term returned.
func TestProcessVoteRequest_StaleTermRejected(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.CurrentTerm = 5
	node.Mu.Unlock()

	resp, err := node.ProcessVoteRequest(structs.VoteReq{
		Term: 3, CandidateID: 2, LastLogIndex: 0, LastLogTerm: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if resp.VoteGranted {
		t.Error("want VoteGranted=false for stale term")
	}
	if resp.Term != 5 {
		t.Errorf("want Term=5, got %d", resp.Term)
	}
}

// TC_0052 — First vote in a new term is granted; VotedFor is persisted.
func TestProcessVoteRequest_GrantsVote_FirstInTerm(t *testing.T) {
	dir := t.TempDir()
	node, _ := domain.NewNode(1, 150, dir)

	resp, err := node.ProcessVoteRequest(structs.VoteReq{
		Term: 1, CandidateID: 2, LastLogIndex: 0, LastLogTerm: 0,
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
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

// TC_0053 — Repeat vote from same candidate in same term is idempotent.
func TestProcessVoteRequest_SameCandidateIdempotent(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2
	node.Mu.Unlock()

	resp, _ := node.ProcessVoteRequest(structs.VoteReq{
		Term: 3, CandidateID: 2, LastLogIndex: 0, LastLogTerm: 0,
	})
	if !resp.VoteGranted {
		t.Error("want VoteGranted=true for repeat request from same candidate")
	}
}

// TC_0054 — Vote request from a different candidate in the same term is denied.
func TestProcessVoteRequest_DeniesVote_DifferentCandidate(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.CurrentTerm = 3
	node.VotedFor[3] = 2
	node.Mu.Unlock()

	resp, _ := node.ProcessVoteRequest(structs.VoteReq{
		Term: 3, CandidateID: 5, LastLogIndex: 0, LastLogTerm: 0,
	})
	if resp.VoteGranted {
		t.Error("want VoteGranted=false when already voted for a different candidate this term")
	}
}

// TC_0055 — Higher term causes node to step down; term is updated before granting.
func TestProcessVoteRequest_HigherTermStepsDown(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.State = "Leader"
	node.CurrentTerm = 3
	node.Mu.Unlock()

	node.ProcessVoteRequest(structs.VoteReq{
		Term: 7, CandidateID: 3, LastLogIndex: 0, LastLogTerm: 0,
	})

	node.Mu.Lock()
	state, term := node.State, node.CurrentTerm
	node.Mu.Unlock()

	if state != "Follower" {
		t.Errorf("State: want 'Follower' after higher-term vote request, got %q", state)
	}
	if term != 7 {
		t.Errorf("CurrentTerm: want 7, got %d", term)
	}
}

// TC_0056 — Candidate with an older log (lower lastLogTerm) is denied (RAFT §5.4).
func TestProcessVoteRequest_OutOfDateLog_Denied(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	// Voter has a more recent log entry (term 5).
	node.Log = append(node.Log, structs.Transaction{Term: 5, ClientID: 1, Payload: "x"})
	node.CurrentTerm = 5
	node.Mu.Unlock()

	resp, _ := node.ProcessVoteRequest(structs.VoteReq{
		Term:         6,
		CandidateID:  2,
		LastLogIndex: 1,
		LastLogTerm:  4, // candidate's last term (4) < voter's (5)
	})
	if resp.VoteGranted {
		t.Error("want VoteGranted=false when candidate log is behind voter log")
	}
}

// ── ProcessAppendEntries ──────────────────────────────────────────────────────

// TC_0057 — Stale term returns success=false without updating state.
func TestProcessAppendEntries_StaleTermRejected(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.CurrentTerm = 5
	node.Mu.Unlock()

	resp, _ := node.ProcessAppendEntries(structs.AppendEntriesReq{
		Term: 3, LeaderID: 2,
	})
	if resp.Success {
		t.Error("want success=false for stale term")
	}
	if resp.Term != 5 {
		t.Errorf("want Term=5, got %d", resp.Term)
	}
}

// TC_0058 — Valid heartbeat resets state to Follower and records the leader.
func TestProcessAppendEntries_HeartbeatUpdatesLeaderState(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.State = "Candidate"
	node.Mu.Unlock()

	resp, err := node.ProcessAppendEntries(structs.AppendEntriesReq{
		Term: 2, LeaderID: 3, Entries: []structs.Transaction{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !resp.Success {
		t.Error("want success=true for valid heartbeat")
	}

	node.Mu.Lock()
	st, lid, term := node.State, node.LeaderId, node.CurrentTerm
	node.Mu.Unlock()

	if st != "Follower" {
		t.Errorf("State: want 'Follower', got %q", st)
	}
	if lid != 3 {
		t.Errorf("LeaderId: want 3, got %d", lid)
	}
	if term != 2 {
		t.Errorf("CurrentTerm: want 2, got %d", term)
	}
}

// TC_0059 — AppendEntries appends new entries to the node log.
func TestProcessAppendEntries_AppendsEntries(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())

	entries := []structs.Transaction{
		{ClientID: 1, Payload: "A->B 50", Term: 1},
		{ClientID: 2, Payload: "C->D 30", Term: 1},
	}
	node.ProcessAppendEntries(structs.AppendEntriesReq{
		Term: 1, LeaderID: 2, PrevLogIndex: 0, PrevLogTerm: 0,
		Entries: entries, LeaderCommit: 0,
	})

	node.Mu.Lock()
	logLen := len(node.Log)
	node.Mu.Unlock()

	if logLen != 2 {
		t.Errorf("want 2 log entries, got %d", logLen)
	}
}

// TC_0060 — PrevLogIndex mismatch causes rejection and log truncation.
func TestProcessAppendEntries_PrevLogMismatch_Rejected(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	// Existing log: one entry at term 1.
	node.Log = append(node.Log, structs.Transaction{Term: 1, ClientID: 1, Payload: "x"})
	node.CurrentTerm = 2
	node.Mu.Unlock()

	// Leader claims PrevLogTerm=2 at index 1, but node has term=1 there.
	resp, _ := node.ProcessAppendEntries(structs.AppendEntriesReq{
		Term: 2, LeaderID: 2, PrevLogIndex: 1, PrevLogTerm: 2,
		Entries: []structs.Transaction{{ClientID: 1, Payload: "y", Term: 2}},
	})
	if resp.Success {
		t.Error("want success=false on PrevLogTerm mismatch")
	}
}

// TC_0061 — Advancing LeaderCommit applies entries to the blockchain buffer.
func TestProcessAppendEntries_AdvancesCommitIndex(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())

	// Append two entries then commit both.
	entries := []structs.Transaction{
		{ClientID: 1, Payload: "2 50", Term: 1},
		{ClientID: 2, Payload: "1 10", Term: 1},
	}
	node.ProcessAppendEntries(structs.AppendEntriesReq{
		Term: 1, LeaderID: 2, Entries: entries, LeaderCommit: 2,
	})

	node.Mu.Lock()
	ci := node.CommitIndex
	la := node.LastApplied
	node.Mu.Unlock()

	if ci != 2 {
		t.Errorf("CommitIndex: want 2, got %d", ci)
	}
	if la != 2 {
		t.Errorf("LastApplied: want 2, got %d", la)
	}
}

// ── AppendTransaction / WaitForCommit ─────────────────────────────────────────

// TC_0062 — AppendTransaction stores entry in log and returns 1-based index.
func TestAppendTransaction_AppendsToLog(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	node.State = "Leader"
	node.CurrentTerm = 1
	node.Mu.Unlock()

	idx, err := node.AppendTransaction(1, "2 100", time.Now().UnixMilli())
	if err != nil {
		t.Fatalf("AppendTransaction: %v", err)
	}
	if idx != 1 {
		t.Errorf("want log index 1, got %d", idx)
	}

	node.Mu.Lock()
	logLen := len(node.Log)
	payload := node.Log[0].Payload
	node.Mu.Unlock()

	if logLen != 1 {
		t.Errorf("want 1 log entry, got %d", logLen)
	}
	if payload != "2 100" {
		t.Errorf("payload: want '2 100', got %q", payload)
	}
}

// TC_0063 — WaitForCommit returns false before CommitIndex advances; returns
// true after manual advancement within the timeout window.
func TestWaitForCommit_ReturnsFalseOnTimeout(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	// CommitIndex stays at 0 — nothing commits it.
	committed := node.WaitForCommit(1, 150*time.Millisecond)
	if committed {
		t.Error("want false when commit never advances within timeout")
	}
}

func TestWaitForCommit_ReturnsTrueWhenCommitted(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())

	// Advance CommitIndex in a goroutine after a short delay.
	go func() {
		time.Sleep(80 * time.Millisecond)
		node.Mu.Lock()
		node.CommitIndex = 1
		node.Mu.Unlock()
	}()

	committed := node.WaitForCommit(1, 500*time.Millisecond)
	if !committed {
		t.Error("want true when CommitIndex reaches the target before timeout")
	}
}

// ── GetBalance ────────────────────────────────────────────────────────────────

// TC_0064 — GetBalance reflects blockchain state; fresh node has zero balance.
func TestGetBalance_EmptyChain_ZeroBalance(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	committed, pending := node.GetBalance(1)
	if committed != 0 {
		t.Errorf("committed balance: want 0 on empty chain, got %d", committed)
	}
	if pending != 0 {
		t.Errorf("pending balance: want 0 on empty chain, got %d", pending)
	}
}

// TC_0065 — GetBalance accounts for uncommitted log entries in the pending balance.
func TestGetBalance_IncludesUncommittedEntries(t *testing.T) {
	node, _ := domain.NewNode(1, 150, t.TempDir())
	node.Mu.Lock()
	// Client 1 sends 100 to client 2; not yet committed.
	node.Log = append(node.Log, structs.Transaction{
		ClientID: 1, Payload: "2 100", Term: 1,
	})
	// CommitIndex stays at 0 so this is "uncommitted" / pending.
	node.Mu.Unlock()

	_, pending := node.GetBalance(1)
	if pending != -100 {
		t.Errorf("pending balance for sender: want -100, got %d", pending)
	}

	_, pending2 := node.GetBalance(2)
	if pending2 != 100 {
		t.Errorf("pending balance for receiver: want 100, got %d", pending2)
	}
}