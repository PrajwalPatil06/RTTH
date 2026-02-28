package handlers

import (
	"RTTH/internal/domain"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"
)
type Handler struct {
	Store store.LogStore
	RaftNode domain.Node
}

//inject dpendency
func NewHandler(s store.LogStore, n domain.Node) *Handler {
	return &Handler{
		Store: s,
		RaftNode: n,
	}
}
func (handler *Handler)HandleAppendTransactionReq(c *gin.Context) {
	var txn structs.ClientTransaction
		err := c.ShouldBindJSON(&txn)
		if err != nil {
			// fmt.Println(err)
			c.JSON(400, gin.H{"error": err.Error()})
			return
		}
	switch handler.RaftNode.State {
	case "Leader":		
		err = txn.Validate() 
		if err != nil {
			// fmt.Println(err)
			c.JSON(400, gin.H{"error": err.Error()})
		}
		c.JSON(200, "Successfully stored")
		handler.Store.Append(structs.Transaction{ID: txn.ClientID, Payload: txn.Payload, Timestamp: txn.Timestamp})

		// print on terminal
		fmt.Println(handler.Store)
	case "Follower":
		// send to leader
		body, err := json.Marshal(txn)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		req, err := http.NewRequest("POST", handler.RaftNode.OtherNodes[handler.RaftNode.LeaderId]+"/append", bytes.NewBuffer(body))
		if err != nil {
			log.Fatal(err)
		}
		req.Header.Set("Content-Type", "application/json")
		client := &http.Client{}
		resp, err := client.Do(req)
		if err != nil {
			c.JSON(500, gin.H{"error": err.Error()})
			return
		}
		defer resp.Body.Close()
	default:
		c.JSON(400, gin.H{"error": "Currently in election phase"})
	}
}
func (handler *Handler) HandleHeartBeat(c *gin.Context) {
	var heartBeat structs.HeartBeat
	err := c.ShouldBindJSON(heartBeat)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
	}
	handler.RaftNode.LastLeaderTimeStamp = heartBeat.Timestamp
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