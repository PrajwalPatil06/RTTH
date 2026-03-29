package handlers

import (
	"RTTH/internal/domain"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Store    store.LogStore
	RaftNode *domain.Node
}

func NewHandler(s store.LogStore, n *domain.Node) *Handler {
	return &Handler{Store: s, RaftNode: n}
}

func (handler *Handler) HandleAppendTransactionReq(c *gin.Context) {
	node := handler.RaftNode
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
		// TODO: append to node.Log, persist, replicate to followers, then
		// commit once a majority acknowledges. For now we write directly to
		// the in-memory store as a placeholder.
		if err := handler.Store.Append(entry); err != nil {
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

func (handler *Handler) HandleVoteRequest(c *gin.Context) {
	var voteReq structs.VoteReq
	if err := c.ShouldBindJSON(&voteReq); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node := handler.RaftNode
	node.Mu.Lock()
	defer node.Mu.Unlock()

	if voteReq.Term < node.CurrentTerm {
		c.JSON(http.StatusOK, structs.VoteResp{Term: node.CurrentTerm, VoteGranted: false})
		return
	}

	if voteReq.Term > node.CurrentTerm {
		node.CurrentTerm = voteReq.Term
		node.State = "Follower"
		node.VotedFor[node.CurrentTerm] = 0
	}

	voteGranted := false
	votedFor := node.VotedFor[node.CurrentTerm]
	if votedFor == 0 || votedFor == voteReq.CandidateID {
		voteGranted = true
		node.VotedFor[node.CurrentTerm] = voteReq.CandidateID
	}

	if voteGranted {
		if err := node.Persist(); err != nil {
			log.Println("persist failed in HandleVoteRequest — refusing vote:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
			return
		}
	}

	c.JSON(http.StatusOK, structs.VoteResp{Term: node.CurrentTerm, VoteGranted: voteGranted})
}

func (handler *Handler) HandleAppendEntries(c *gin.Context) {
	var req structs.AppendEntriesReq
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}

	node := handler.RaftNode
	node.Mu.Lock()
	defer node.Mu.Unlock()

	if req.Term < node.CurrentTerm {
		c.JSON(http.StatusOK, structs.AppendEntriesResp{Term: node.CurrentTerm, Success: false})
		return
	}

	changed := false
	if req.Term > node.CurrentTerm {
		node.CurrentTerm = req.Term
		node.VotedFor[node.CurrentTerm] = 0
		changed = true
	}

	node.State = "Follower"
	node.LeaderId = req.LeaderID
	node.LastLeaderTimeStamp = time.Now().UnixMilli()

	// TODO: validate PrevLogIndex/PrevLogTerm, append req.Entries to node.Log
	// For now we accept heartbeats (empty entries) unconditionally.
	if len(req.Entries) > 0 {
		node.Log = append(node.Log, req.Entries...)
		changed = true
	}

	if changed {
		if err := node.Persist(); err != nil {
			log.Println("persist failed in HandleAppendEntries:", err)
			c.JSON(http.StatusInternalServerError, gin.H{"error": "storage error"})
			return
		}
	}

	log.Printf("AppendEntries from leader %d (term %d), %d entries", req.LeaderID, req.Term, len(req.Entries))
	c.JSON(http.StatusOK, structs.AppendEntriesResp{Term: node.CurrentTerm, Success: true})
}

func (handler *Handler) GetUserDetails(c *gin.Context) {
	var req structs.GetRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	if req.ClientID == 0 {
		c.JSON(http.StatusBadRequest, gin.H{"error": "clientid is required"})
		return
	}
	txnDetails, err := handler.Store.GetByID(req.ClientID)
	if err != nil {
		c.JSON(http.StatusNotFound, gin.H{"error": err.Error()})
		return
	}
	c.JSON(http.StatusOK, txnDetails)
}

func (handler *Handler) GetAllUserDetails(c *gin.Context) {
	c.JSON(http.StatusOK, handler.Store.GetAll())
}