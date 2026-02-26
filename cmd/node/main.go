package main

import (
	"RTTH/internal/handlers"
	//"RTTH/internal/store"
	"RTTH/internal/domain"

	"github.com/gin-gonic/gin"
)

func main() {
	RaftNode := domain.NewNode(1)
	txHandler := handlers.NewTransactionHandler(RaftNode.Store)
	router := gin.Default()
	router.POST("/append", txHandler.SubmitTransaction)
	router.Run(":8080")
}