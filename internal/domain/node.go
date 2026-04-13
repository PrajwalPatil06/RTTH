package domain

import (
	"RTTH/internal/blockchain"
	"RTTH/internal/persist"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// Node is a single RAFT participant that also maintains a blockchain.
type Node struct {
	Mu sync.Mutex

	Id                  int
	State               string
	Store               *store.MemoryStore
	OtherNodes          map[int]string // nodeId -> channel-proxy URL
	LeaderId            int
	LastLeaderTimeStamp int64
	Timeout             int

	// ── Persistent state (written before responding to RPCs) ──
	CurrentTerm int
	VotedFor    map[int]int           // map[term]votedForCandidateId
	Log         []structs.Transaction // 0-based slice; RAFT indices are 1-based

	// ── Volatile state (reset on restart) ──
	CommitIndex int
	LastApplied int

	// ── Blockchain state (also persisted) ──
	Blockchain  []structs.Block       // committed, mined blocks
	BlockBuffer []structs.Transaction // committed txns waiting to fill the next block

	// ── Leader-only volatile state (reset on every election win) ──
	nextIndex  map[int]int
	matchIndex map[int]int

	storage *persist.Storage
}

// NewNode creates a node, loads any previously persisted state, and—on first
// boot—seeds the blockchain from first_blockchain.txt.
func NewNode(id int, timeoutMs int, dataDir string) (*Node, error) {
	randomizedTimeout := timeoutMs + rand.Intn(timeoutMs)

	storage, err := persist.NewStorage(dataDir, id)
	if err != nil {
		return nil, err
	}

	saved, err := storage.Load()
	if err != nil {
		return nil, err
	}

	// One-time seed from first_blockchain.txt when the chain is empty.
	if len(saved.Blockchain) == 0 {
		if chain, loadErr := blockchain.LoadFirstBlockchain("first_blockchain.txt"); loadErr == nil && len(chain) > 0 {
			saved.Blockchain = chain
			_ = storage.Save(persist.State{
				CurrentTerm: saved.CurrentTerm,
				VotedFor:    saved.VotedFor,
				Log:         saved.Log,
				Blockchain:  chain,
				BlockBuffer: saved.BlockBuffer,
			})
		}
	}

	return &Node{
		Id:    id,
		State: "Follower",
		Store: store.NewMemoryStore(),
		OtherNodes: map[int]string{
			1: "http://localhost:9000/forward/1",
			2: "http://localhost:9000/forward/2",
			3: "http://localhost:9000/forward/3",
		},
		LeaderId:            0,
		LastLeaderTimeStamp: time.Now().UnixMilli() + int64(randomizedTimeout),
		Timeout:             randomizedTimeout,

		CurrentTerm: saved.CurrentTerm,
		VotedFor:    saved.VotedFor,
		Log:         saved.Log,

		CommitIndex: 0,
		LastApplied: 0,

		Blockchain:  saved.Blockchain,
		BlockBuffer: saved.BlockBuffer,

		nextIndex:  make(map[int]int),
		matchIndex: make(map[int]int),

		storage: storage,
	}, nil
}

// Persist writes the current durable state to disk.
// Caller must hold n.Mu.
func (n *Node) Persist() error {
	return n.storage.Save(persist.State{
		CurrentTerm: n.CurrentTerm,
		VotedFor:    n.VotedFor,
		Log:         n.Log,
		Blockchain:  n.Blockchain,
		BlockBuffer: n.BlockBuffer,
	})
}

// ── Log helpers ───────────────────────────────────────────────────────────────

func (n *Node) lastLogIndex() int {
	return len(n.Log)
}

func (n *Node) lastLogTerm() int {
	if len(n.Log) == 0 {
		return 0
	}
	return n.Log[len(n.Log)-1].Term
}

// uncommitted returns log entries that have not yet been committed (may be nil).
func (n *Node) uncommitted() []structs.Transaction {
	if n.CommitIndex >= len(n.Log) {
		return nil
	}
	return n.Log[n.CommitIndex:]
}

// ── Blockchain apply ──────────────────────────────────────────────────────────

// applyCommittedLocked drains newly committed log entries into blockchain blocks.
// Must be called with n.Mu held.  Mining typically completes in <1 ms.
func (n *Node) applyCommittedLocked() {
	for n.LastApplied < n.CommitIndex {
		txn := n.Log[n.LastApplied]
		n.LastApplied++
		n.BlockBuffer = append(n.BlockBuffer, txn)

		if len(n.BlockBuffer) >= structs.BlockSize {
			batch := n.BlockBuffer[:structs.BlockSize]
			blockTerm := batch[structs.BlockSize-1].Term
			block := blockchain.BuildBlock(n.Blockchain, batch, blockTerm)
			n.Blockchain = append(n.Blockchain, block)
			n.BlockBuffer = n.BlockBuffer[structs.BlockSize:]
			log.Printf("[Node %d] mined block #%d — chain: %s",
				n.Id, len(n.Blockchain), blockchain.PrintChain(n.Blockchain))
		}
	}
}

// ── Leader helpers ────────────────────────────────────────────────────────────

func (n *Node) initLeaderState() {
	n.nextIndex = make(map[int]int)
	n.matchIndex = make(map[int]int)
	for id := range n.OtherNodes {
		n.nextIndex[id] = n.lastLogIndex() + 1
		n.matchIndex[id] = 0
	}
}

// advanceCommitIndex advances CommitIndex to the highest index where a
// majority of nodes have replicated the entry (RAFT §5.3/§5.4).
// Must be called with n.Mu held.
func (n *Node) advanceCommitIndex() {
	majority := (len(n.OtherNodes)+1)/2 + 1
	for idx := n.CommitIndex + 1; idx <= n.lastLogIndex(); idx++ {
		if n.Log[idx-1].Term != n.CurrentTerm {
			continue
		}
		count := 1 // self always replicates
		for _, mi := range n.matchIndex {
			if mi >= idx {
				count++
			}
		}
		if count >= majority {
			n.CommitIndex = idx
		}
	}
	n.applyCommittedLocked()
}

// ── Run loop ──────────────────────────────────────────────────────────────────

// Run is the RAFT state-machine loop.  Start it in a goroutine.
func (n *Node) Run() {
	n.Mu.Lock()
	delete(n.OtherNodes, n.Id)
	n.Mu.Unlock()

	for {
		n.Mu.Lock()
		state := n.State
		lastLeader := n.LastLeaderTimeStamp
		timeout := n.Timeout
		n.Mu.Unlock()

		switch state {
		case "Leader":
			time.Sleep(time.Duration(timeout/5) * time.Millisecond)
			n.replicateLog()
		case "Follower":
			time.Sleep(50 * time.Millisecond)
			if time.Now().UnixMilli()-lastLeader > int64(timeout) {
				log.Printf("[Node %d] election timeout — starting election", n.Id)
				n.StartElection()
			}
		case "Candidate":
			time.Sleep(50 * time.Millisecond)
		}
	}
}

// ── Log replication ───────────────────────────────────────────────────────────

// replicateLog sends AppendEntries to every peer (heartbeat + real entries).
func (n *Node) replicateLog() {
	n.Mu.Lock()
	if n.State != "Leader" {
		n.Mu.Unlock()
		return
	}

	type task struct {
		nodeID int
		url    string
		req    structs.AppendEntriesReq
	}

	selfID := n.Id
	var tasks []task
	for nodeID, url := range n.OtherNodes {
		ni := n.nextIndex[nodeID]
		if ni < 1 {
			ni = 1
		}
		prevIdx := ni - 1
		prevTerm := 0
		if prevIdx > 0 && prevIdx <= len(n.Log) {
			prevTerm = n.Log[prevIdx-1].Term
		}
		var entries []structs.Transaction
		if ni <= len(n.Log) {
			entries = make([]structs.Transaction, len(n.Log)-ni+1)
			copy(entries, n.Log[ni-1:])
		}
		tasks = append(tasks, task{
			nodeID: nodeID, url: url,
			req: structs.AppendEntriesReq{
				Term:         n.CurrentTerm,
				LeaderID:     selfID,
				PrevLogIndex: prevIdx,
				PrevLogTerm:  prevTerm,
				Entries:      entries,
				LeaderCommit: n.CommitIndex,
			},
		})
	}
	n.Mu.Unlock()

	type result struct {
		nodeID   int
		success  bool
		respTerm int
		matchIdx int
	}
	ch := make(chan result, len(tasks))
	cli := &http.Client{Timeout: 200 * time.Millisecond}

	for _, t := range tasks {
		go func(t task) {
			body, err := json.Marshal(t.req)
			if err != nil {
				ch <- result{nodeID: t.nodeID}
				return
			}
			req, err := http.NewRequest(http.MethodPost, t.url+"/appendentries", bytes.NewReader(body))
			if err != nil {
				ch <- result{nodeID: t.nodeID}
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Sender-ID", fmt.Sprintf("%d", selfID))

			resp, err := cli.Do(req)
			if err != nil {
				ch <- result{nodeID: t.nodeID}
				return
			}
			defer resp.Body.Close()

			var aeResp structs.AppendEntriesResp
			if err := json.NewDecoder(resp.Body).Decode(&aeResp); err != nil {
				ch <- result{nodeID: t.nodeID}
				return
			}
			ch <- result{
				nodeID:   t.nodeID,
				success:  aeResp.Success,
				respTerm: aeResp.Term,
				matchIdx: t.req.PrevLogIndex + len(t.req.Entries),
			}
		}(t)
	}

	for range tasks {
		r := <-ch
		n.Mu.Lock()
		if r.respTerm > n.CurrentTerm {
			n.CurrentTerm = r.respTerm
			n.State = "Follower"
			n.LeaderId = 0
			_ = n.Persist()
			n.Mu.Unlock()
			continue
		}
		if n.State != "Leader" {
			n.Mu.Unlock()
			continue
		}
		if r.success {
			if r.matchIdx > n.matchIndex[r.nodeID] {
				n.matchIndex[r.nodeID] = r.matchIdx
			}
			n.nextIndex[r.nodeID] = n.matchIndex[r.nodeID] + 1
		} else if n.nextIndex[r.nodeID] > 1 {
			n.nextIndex[r.nodeID]--
		}
		n.advanceCommitIndex()
		n.Mu.Unlock()
	}
}

// ── Election ──────────────────────────────────────────────────────────────────

// StartElection runs a full RAFT leader election from this node.
func (n *Node) StartElection() {
	n.Mu.Lock()
	n.State = "Candidate"
	n.CurrentTerm++
	n.LastLeaderTimeStamp = time.Now().UnixMilli()
	n.VotedFor[n.CurrentTerm] = n.Id
	if err := n.Persist(); err != nil {
		log.Printf("[Node %d] persist failed at election start: %v", n.Id, err)
		n.State = "Follower"
		n.Mu.Unlock()
		return
	}
	term := n.CurrentTerm
	selfID := n.Id
	lastIdx := n.lastLogIndex()
	lastTrm := n.lastLogTerm()
	peers := make(map[int]string, len(n.OtherNodes))
	for k, v := range n.OtherNodes {
		peers[k] = v
	}
	required := (len(peers)+1)/2 + 1
	n.Mu.Unlock()

	log.Printf("[Node %d] election started (term=%d need=%d)", selfID, term, required)
	votes := 1

	voteBody, err := json.Marshal(structs.VoteReq{
		Term:         term,
		CandidateID:  selfID,
		LastLogIndex: lastIdx,
		LastLogTerm:  lastTrm,
		Timestamp:    time.Now().UnixMilli(),
	})
	if err != nil {
		n.Mu.Lock()
		if n.State == "Candidate" {
			n.State = "Follower"
		}
		n.Mu.Unlock()
		return
	}

	voteCh := make(chan bool, len(peers))
	cli := &http.Client{Timeout: 200 * time.Millisecond}

	for peerID, peerURL := range peers {
		go func(pid int, url string) {
			req, err := http.NewRequest(http.MethodPost, url+"/requestvote", bytes.NewReader(voteBody))
			if err != nil {
				voteCh <- false
				return
			}
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("X-Sender-ID", fmt.Sprintf("%d", selfID))

			resp, err := cli.Do(req)
			if err != nil {
				log.Printf("[Node %d] vote req to %d failed: %v", selfID, pid, err)
				voteCh <- false
				return
			}
			defer resp.Body.Close()

			var vr structs.VoteResp
			if err := json.NewDecoder(resp.Body).Decode(&vr); err != nil {
				voteCh <- false
				return
			}
			n.Mu.Lock()
			if vr.Term > n.CurrentTerm {
				n.CurrentTerm = vr.Term
				n.State = "Follower"
				n.LeaderId = 0
				_ = n.Persist()
			}
			n.Mu.Unlock()
			voteCh <- vr.VoteGranted
		}(peerID, peerURL)
	}

	for range peers {
		if <-voteCh {
			votes++
		}
		n.Mu.Lock()
		if n.State != "Candidate" {
			n.Mu.Unlock()
			return
		}
		if votes >= required {
			n.State = "Leader"
			n.LeaderId = selfID
			n.initLeaderState()
			n.Mu.Unlock()
			log.Printf("[Node %d] became Leader (term=%d)", selfID, term)
			n.replicateLog()
			return
		}
		n.Mu.Unlock()
	}

	n.Mu.Lock()
	if n.State == "Candidate" {
		n.State = "Follower"
	}
	n.Mu.Unlock()
	log.Printf("[Node %d] election failed (quorum not reached)", selfID)
}

// ── Public helpers used by HTTP handlers ──────────────────────────────────────

// ProcessVoteRequest evaluates an incoming VoteReq and returns the response.
// It acquires the mutex internally; do not call while holding it.
func (n *Node) ProcessVoteRequest(req structs.VoteReq) (structs.VoteResp, error) {
	n.Mu.Lock()
	defer n.Mu.Unlock()

	if req.Term < n.CurrentTerm {
		return structs.VoteResp{Term: n.CurrentTerm, VoteGranted: false}, nil
	}
	if req.Term > n.CurrentTerm {
		n.CurrentTerm = req.Term
		n.State = "Follower"
		n.VotedFor[n.CurrentTerm] = 0
	}

	// RAFT §5.4: only grant if candidate log is at least as up-to-date.
	logOK := req.LastLogTerm > n.lastLogTerm() ||
		(req.LastLogTerm == n.lastLogTerm() && req.LastLogIndex >= n.lastLogIndex())
	votedFor := n.VotedFor[n.CurrentTerm]
	grant := logOK && (votedFor == 0 || votedFor == req.CandidateID)
	if grant {
		n.VotedFor[n.CurrentTerm] = req.CandidateID
		if err := n.Persist(); err != nil {
			return structs.VoteResp{Term: n.CurrentTerm, VoteGranted: false}, err
		}
	}
	return structs.VoteResp{Term: n.CurrentTerm, VoteGranted: grant}, nil
}

// ProcessAppendEntries handles the full AppendEntries RPC (RAFT §5.3).
// It acquires the mutex internally; do not call while holding it.
func (n *Node) ProcessAppendEntries(req structs.AppendEntriesReq) (structs.AppendEntriesResp, error) {
	n.Mu.Lock()
	defer n.Mu.Unlock()

	if req.Term < n.CurrentTerm {
		return structs.AppendEntriesResp{Term: n.CurrentTerm, Success: false}, nil
	}
	changed := false
	if req.Term > n.CurrentTerm {
		n.CurrentTerm = req.Term
		n.VotedFor[n.CurrentTerm] = 0
		changed = true
	}
	n.State = "Follower"
	n.LeaderId = req.LeaderID
	n.LastLeaderTimeStamp = time.Now().UnixMilli()

	// PrevLog consistency check (§5.3).
	if req.PrevLogIndex > 0 {
		if req.PrevLogIndex > len(n.Log) {
			if changed {
				_ = n.Persist()
			}
			return structs.AppendEntriesResp{Term: n.CurrentTerm, Success: false}, nil
		}
		if n.Log[req.PrevLogIndex-1].Term != req.PrevLogTerm {
			// Truncate the conflicting suffix and retry.
			n.Log = n.Log[:req.PrevLogIndex-1]
			if err := n.Persist(); err != nil {
				return structs.AppendEntriesResp{}, err
			}
			return structs.AppendEntriesResp{Term: n.CurrentTerm, Success: false}, nil
		}
	}

	// Append / overwrite entries.
	if len(req.Entries) > 0 {
		for i, entry := range req.Entries {
			pos := req.PrevLogIndex + i + 1
			if pos <= len(n.Log) {
				if n.Log[pos-1].Term != entry.Term {
					n.Log = append(n.Log[:pos-1], req.Entries[i:]...)
					break
				}
			} else {
				n.Log = append(n.Log, entry)
			}
		}
		changed = true
	}

	// Advance commitIndex to whatever the leader says.
	if req.LeaderCommit > n.CommitIndex {
		newCI := req.LeaderCommit
		if newCI > len(n.Log) {
			newCI = len(n.Log)
		}
		n.CommitIndex = newCI
		n.applyCommittedLocked()
		changed = true
	}

	if changed {
		if err := n.Persist(); err != nil {
			return structs.AppendEntriesResp{}, err
		}
	}

	log.Printf("[Node %d] AppendEntries ok (leader=%d term=%d entries=%d ci=%d)",
		n.Id, req.LeaderID, req.Term, len(req.Entries), n.CommitIndex)
	return structs.AppendEntriesResp{Term: n.CurrentTerm, Success: true}, nil
}

// AppendTransaction appends a new entry to the leader's log and persists it.
// Returns the 1-based log index of the new entry.
// Caller must NOT hold n.Mu.
func (n *Node) AppendTransaction(clientID int, payload string, timestamp int64) (int, error) {
	n.Mu.Lock()
	defer n.Mu.Unlock()

	entry := structs.Transaction{
		ClientID:  clientID,
		Payload:   payload,
		Timestamp: timestamp,
		Term:      n.CurrentTerm,
	}
	n.Log = append(n.Log, entry)
	idx := len(n.Log)
	if err := n.Persist(); err != nil {
		n.Log = n.Log[:idx-1]
		return 0, err
	}
	return idx, nil
}

// WaitForCommit blocks until CommitIndex ≥ logIndex or timeout expires.
func (n *Node) WaitForCommit(logIndex int, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		n.Mu.Lock()
		ok := n.CommitIndex >= logIndex
		n.Mu.Unlock()
		if ok {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// GetBalance returns the committed and pending balance for clientID.
// Caller must NOT hold n.Mu.
func (n *Node) GetBalance(clientID int) (committed, pending int) {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	committed = blockchain.GetCommittedBalance(n.Blockchain, clientID)
	pending = blockchain.GetPendingBalance(n.Blockchain, n.uncommitted(), clientID)
	return
}

// GetBlockchain returns a deep copy of the current blockchain.
func (n *Node) GetBlockchain() []structs.Block {
	n.Mu.Lock()
	defer n.Mu.Unlock()
	snap := make([]structs.Block, len(n.Blockchain))
	copy(snap, n.Blockchain)
	return snap
}