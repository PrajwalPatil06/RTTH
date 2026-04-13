package handlers

import (
	"RTTH/internal/domain"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

// Handler wires together the in-memory log store and the RAFT node.
type Handler struct {
	Store    store.LogStore
	RaftNode *domain.Node
}

// NewHandler creates a Handler.
func NewHandler(s store.LogStore, n *domain.Node) *Handler {
	return &Handler{Store: s, RaftNode: n}
}

// ── Legacy /append endpoint ───────────────────────────────────────────────────
// Kept for backward-compatibility with existing tests.  The in-memory store
// is written directly; for fully replicated transfers use /transfer instead.

func (h *Handler) HandleAppendTransactionReq(c *gin.Context) {
	node := h.RaftNode
	var txn structs.ClientTransaction
	if err := c.ShouldBindJSON(&txn); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node.Mu.Lock()
	state := node.State
	leaderURL := node.OtherNodes[node.LeaderId]
	node.Mu.Unlock()

	switch state {
	case "Leader":
		if err := txn.Validate(); err != nil {
			c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
			return
		}
		entry := structs.Transaction{
			ClientID:  txn.ClientID,
			Payload:   txn.Payload,
			Timestamp: txn.Timestamp,
		}
		if err := h.Store.Append(entry); err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		c.JSON(http.StatusOK, gin.H{"status": "successfully stored"})

	case "Follower":
		if leaderURL == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no known leader, election in progress"})
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, leaderURL+"/append")

	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "election in progress, no leader available"})
	}
}

// ── RAFT RPCs ─────────────────────────────────────────────────────────────────

// HandleVoteRequest processes a RequestVote RPC.
func (h *Handler) HandleVoteRequest(c *gin.Context) {
	var req structs.VoteReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.RaftNode.ProcessVoteRequest(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// HandleAppendEntries processes an AppendEntries RPC (heartbeat or replication).
func (h *Handler) HandleAppendEntries(c *gin.Context) {
	var req structs.AppendEntriesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	resp, err := h.RaftNode.ProcessAppendEntries(req)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
		return
	}
	c.JSON(http.StatusOK, resp)
}

// ── Legacy store-based read endpoints ────────────────────────────────────────

// GetUserDetails looks up a transaction in the in-memory store by log index.
func (h *Handler) GetUserDetails(c *gin.Context) {
	var req structs.GetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ClientID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "clientid is required"})
		return
	}
	txn, err := h.Store.GetByID(req.ClientID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, txn)
}

// GetAllUserDetails returns the entire in-memory store keyed by log index.
func (h *Handler) GetAllUserDetails(c *gin.Context) {
	c.JSON(http.StatusOK, h.Store.GetAll())
}

// ── Banking endpoints (RAFT-replicated) ──────────────────────────────────────

// HandleTransfer accepts a banking transfer, appends it to the RAFT log on the
// leader, waits up to 3 s for a majority commit, and returns the new balances.
// Followers redirect the client to the current leader.
func (h *Handler) HandleTransfer(c *gin.Context) {
	var txn structs.ClientTransaction
	if err := c.ShouldBindJSON(&txn); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if err := txn.Validate(); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node := h.RaftNode
	node.Mu.Lock()
	state := node.State
	leaderURL := node.OtherNodes[node.LeaderId]
	node.Mu.Unlock()

	switch state {
	case "Leader":
		idx, err := node.AppendTransaction(txn.ClientID, txn.Payload, txn.Timestamp)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}
		committed := node.WaitForCommit(idx, 3*time.Second)
		committedBal, pendingBal := node.GetBalance(txn.ClientID)
		c.JSON(http.StatusOK, gin.H{
			"committed":         committed,
			"committed_balance": committedBal,
			"pending_balance":   pendingBal,
		})

	case "Follower":
		if leaderURL == "" {
			c.JSON(http.StatusServiceUnavailable, gin.H{"error": "no known leader, election in progress"})
			return
		}
		c.Redirect(http.StatusTemporaryRedirect, leaderURL+"/transfer")

	default:
		c.JSON(http.StatusServiceUnavailable, gin.H{"error": "election in progress, no leader available"})
	}
}

// HandleBalance returns the committed and pending balance for a client.
// This is a read-only operation served by any node.
func (h *Handler) HandleBalance(c *gin.Context) {
	var req structs.GetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ClientID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "clientid is required"})
		return
	}
	committedBal, pendingBal := h.RaftNode.GetBalance(req.ClientID)
	c.JSON(http.StatusOK, gin.H{
		"client_id":         req.ClientID,
		"committed_balance": committedBal,
		"pending_balance":   pendingBal,
	})
}

// HandleGetBlockchain returns a snapshot of the node's current blockchain.
func (h *Handler) HandleGetBlockchain(c *gin.Context) {
	c.JSON(http.StatusOK, h.RaftNode.GetBlockchain())
}