package main

import (
	"RTTH/internal/domain"
	"RTTH/internal/handlers"
	"fmt"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// go run main.go nodeId portNo timeoutMs dataDir
func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: main <nodeId> <port> <timeoutMs> <dataDir>")
		os.Exit(1)
	}

	nodeId, err := strconv.Atoi(os.Args[1])
	if err != nil || nodeId <= 0 {
		fmt.Fprintln(os.Stderr, "nodeId must be a positive integer")
		os.Exit(1)
	}

	port := os.Args[2]

	nodeTimeout, err := strconv.Atoi(os.Args[3])
	if err != nil || nodeTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeoutMs must be a positive integer")
		os.Exit(1)
	}

	dataDir := os.Args[4] // e.g. /var/data/raft or ./data

	raftNode, err := domain.NewNode(nodeId, nodeTimeout, dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise node: %v\n", err)
		os.Exit(1)
	}

	handler := handlers.NewHandler(raftNode.Store, raftNode)
	router := gin.Default()
	go raftNode.Run()

	router.POST("/append", handler.HandleAppendTransactionReq)
	router.POST("/appendentries", handler.HandleAppendEntries)
	router.POST("/requestvote", handler.HandleVoteRequest)
	router.POST("/getuserdetails", handler.GetUserDetails)
	router.GET("/getalluserdetails", handler.GetAllUserDetails)

	router.Run(":" + port)
}