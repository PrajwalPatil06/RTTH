package handlers

import (
	"RTTH/internal/structs"
	"RTTH/internal/store"
	"github.com/gin-gonic/gin"

)
type TransactionHandler struct {
	Store store.LogStore
}

//inject dpendency
func NewTransactionHandler(s store.LogStore) *TransactionHandler {
	return &TransactionHandler{
		Store: s,
	}
}
func (txHandler *TransactionHandler)SubmitTransaction(c *gin.Context) {
	var txn structs.TransactionRequest
	err := c.ShouldBindJSON(&txn)
	if err != nil {
		c.JSON(400, gin.H{"error": err.Error()})
		return
	}
	c.JSON(200,"Yayy")
	


}
