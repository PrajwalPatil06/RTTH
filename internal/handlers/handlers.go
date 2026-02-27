package handlers

import (
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"fmt"
	"github.com/gin-gonic/gin"
)
type NodeStore struct {
	Store store.LogStore
}

//inject dpendency
func NewNodeStore(s store.LogStore) *NodeStore {
	return &NodeStore{
		Store: s,
	}
}
func (nodeStorage *NodeStore)SubmitTransaction(c *gin.Context) {
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
	c.JSON(200, "Successfully stored")
	nodeStorage.Store.Append(structs.Transaction{ID: txn.ClientID, Payload: txn.Payload, Timestamp: txn.Timestamp})

	// print on terminal
	fmt.Println(nodeStorage.Store)
}

func (nodeStorage *NodeStore) GetUserDetails(c *gin.Context) {
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
	txnDetails, err := nodeStorage.Store.GetByID(txn.ClientID)
	c.JSON(200, txnDetails)
}

func (nodeStorage *NodeStore) GetAllUserDetails(c *gin.Context) {
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
	c.JSON(200, nodeStorage.Store.GetAll())
}