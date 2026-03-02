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

// inject dpendency
func NewHandler(s store.LogStore, n *domain.Node) *Handler {
	return &Handler{
		Store:    s,
		RaftNode: n,
	}
}
func (handler *Handler) HandleAppendTransactionReq(c *gin.Context) {
	node := handler.RaftNode
	var txn structs.ClientTransaction
	err := c.ShouldBindJSON(&txn)
	if err != nil {
		// fmt.Println(err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	switch node.State {
	case "Leader":
		err = txn.Validate()
		if err != nil {
			// fmt.Println(err)
			c.JSON(400, gin.H{"error": err.Error()})
		}
		handler.Store.Append(structs.Transaction{ID: txn.ClientID, Payload: txn.Payload, Timestamp: txn.Timestamp})
		//crash here : handle by req id map
		c.JSON(200, "Successfully stored")

		// print on terminal
		fmt.Println(handler.Store)
	case "Follower":
		// redirect to leader
		c.Redirect(http.StatusTemporaryRedirect, node.OtherNodes[node.LeaderId]+"/append")
		return
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
	// If candidate's term is higher, step down to Follower
	if voteReq.Term > node.CurrentTerm {
		node.CurrentTerm = voteReq.Term
		node.State = "Follower"
		// Clear previous votes for this new term
		node.VotedFor[node.CurrentTerm] = 0
	}

	// Grant vote if terms match and we haven't voted for someone else yet
	if voteReq.Term == node.CurrentTerm {
		votedFor := node.VotedFor[node.CurrentTerm]
		if votedFor == 0 || votedFor == voteReq.CandidateID {
			voteGranted = true
			node.VotedFor[node.CurrentTerm] = voteReq.CandidateID
			// Reset election timeout because we acknowledge a viable candidate
			node.LastLeaderTimeStamp = time.Now().UnixMilli()
		}
	}

	// Send the response back
	c.JSON(200, structs.VoteResp{VoteGranted: voteGranted})

}
func (handler *Handler) HandleHeartBeat(c *gin.Context) {
	var heartBeat structs.HeartBeat
	err := c.ShouldBindJSON(&heartBeat)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		fmt.Println(err)
	}
	handler.RaftNode.LastLeaderTimeStamp = heartBeat.Timestamp
	log.Printf("Received heartbeat from %d with timestamp %d", heartBeat.LeaderID, heartBeat.Timestamp)
}
func (handler *Handler) GetUserDetails(c *gin.Context) {
	var txn structs.ClientTransaction
	err := c.ShouldBindJSON(&txn)
	if err != nil {
		// fmt.Println(err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	err = txn.Validate()
	if err != nil {
		// fmt.Println(err)
		c.JSON(400, gin.H{"error": err.Error()})
	}
	txnDetails, err := handler.Store.GetByID(txn.ClientID)
	c.JSON(200, txnDetails)
}

func (handler *Handler) GetAllUserDetails(c *gin.Context) {
	var txn structs.ClientTransaction
	err := c.ShouldBindJSON(&txn)
	if err != nil {
		// fmt.Println(err)
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	err = txn.Validate()
	if err != nil {
		// fmt.Println(err)
		c.JSON(400, gin.H{"error": err.Error()})
	}
	c.JSON(200, handler.Store.GetAll())
}
