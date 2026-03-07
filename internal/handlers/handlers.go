package handlers

import (
	"RTTH/internal/domain"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
)

type Handler struct {
	Store    store.LogStore
	RaftNode *domain.Node
}

// inject dependency
func NewHandler(s store.LogStore, n *domain.Node) *Handler {
	return &Handler{
		Store:    s,
		RaftNode: n,
	}
}

func (handler *Handler) HandleAppendTransactionReq(c *gin.Context) {
	node := handler.RaftNode
	var txn structs.ClientTransaction
	if err := c.ShouldBindJSON(&txn); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	node.Mu.Lock()
	state := node.State
	leaderURL := node.OtherNodes[node.LeaderId]
	node.Mu.Unlock()

	switch state {
	case "Leader":
		if err := txn.Validate(); err != nil {
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
		handler.Store.Append(structs.Transaction{ID: txn.ClientID, Payload: txn.Payload, Timestamp: txn.Timestamp})
		c.JSON(200, "Successfully stored")
		fmt.Println(handler.Store)
	case "Follower":
		c.Redirect(http.StatusTemporaryRedirect, leaderURL+"/append")
	default:
		c.JSON(400, gin.H{"error": "Currently in election phase"})
	}
}

func (handler *Handler) HandleVoteRequest(c *gin.Context) {
	var voteReq structs.VoteReq
	if err := c.ShouldBindJSON(&voteReq); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	node := handler.RaftNode
	voteGranted := false

	node.Mu.Lock()
	if voteReq.Term > node.CurrentTerm {
		node.CurrentTerm = voteReq.Term
		node.State = "Follower"
		node.VotedFor[node.CurrentTerm] = 0
	}
	if voteReq.Term == node.CurrentTerm {
		votedFor := node.VotedFor[node.CurrentTerm]
		if votedFor == 0 || votedFor == voteReq.CandidateID {
			voteGranted = true
			node.VotedFor[node.CurrentTerm] = voteReq.CandidateID
			node.LastLeaderTimeStamp = time.Now().UnixMilli()
		}
	}
	node.Mu.Unlock()

	c.JSON(200, structs.VoteResp{VoteGranted: voteGranted})
}

func (handler *Handler) HandleHeartBeat(c *gin.Context) {
	var heartBeat structs.HeartBeat
	if err := c.ShouldBindJSON(&heartBeat); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}

	node := handler.RaftNode
	node.Mu.Lock()
	node.LastLeaderTimeStamp = time.Now().UnixMilli() // use local clock, not leader's
	node.Mu.Unlock()

	log.Printf("Received heartbeat from %d with timestamp %d", heartBeat.LeaderID, heartBeat.Timestamp)
	c.JSON(200, gin.H{"status": "ok"})
}

func (handler *Handler) GetUserDetails(c *gin.Context) {
	var txn structs.ClientTransaction
	if err := c.ShouldBindJSON(&txn); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := txn.Validate(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	txnDetails, err := handler.Store.GetByID(txn.ClientID)
	if err != nil {
		c.JSON(500, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, txnDetails)
}

func (handler *Handler) GetAllUserDetails(c *gin.Context) {
	var txn structs.ClientTransaction
	if err := c.ShouldBindJSON(&txn); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	if err := txn.Validate(); err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200, handler.Store.GetAll())
}
