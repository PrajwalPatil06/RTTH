package main

import (
	"RTTH/internal/handlers"
	"os"
	"strconv"

	//"RTTH/internal/store"
	"RTTH/internal/domain"

	"github.com/gin-gonic/gin"
)

func main() {
	// hard coded 1 node
	nodeId,_ := strconv.Atoi(os.Args[1])
	RaftNode := domain.NewNode(nodeId)

	handler := handlers.NewHandler(RaftNode.Store,*RaftNode)
	router := gin.Default()
	go RaftNode.Run()
	router.POST("/append", handler.HandleAppendTransactionReq)
	router.POST("/heartbeat", handler.HandleHeartBeat)
	router.POST("/getuserdetails", handler.GetUserDetails)
	router.POST("/getalluserdetails", handler.GetAllUserDetails)
	router.Run(":"+os.Args[2])
}