package main

import (
	"RTTH/internal/handlers"
	//"RTTH/internal/store"
	"RTTH/internal/domain"

	"github.com/gin-gonic/gin"
)

func main() {
	// hard coded 1 node
	RaftNode := domain.NewNode(1)
	nodeStorage := handlers.NewNodeStore(RaftNode.Store)
	router := gin.Default()
	router.POST("/append", nodeStorage.SubmitTransaction)
	router.POST("/getuserdetails", nodeStorage.GetUserDetails)
	router.POST("/getalluserdetails", nodeStorage.GetAllUserDetails)
	router.Run(":8080")
}