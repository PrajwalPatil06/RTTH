package main

import (
	"RTTH/internal/domain"
	"RTTH/internal/handlers"
	"fmt"
	"os"
	"strconv"

	"github.com/gin-gonic/gin"
)

// Usage: go run main.go <nodeId> <client_port> <server_port> <timeoutMs> <dataDir>
// Example: go run main.go 1 8081 300 ./data/node1
func main() {
	if len(os.Args) < 5 {
		fmt.Fprintln(os.Stderr, "usage: main <nodeId> <client_port> <server_port> <timeoutMs> <dataDir>")
		os.Exit(1)
	}

	nodeId, err := strconv.Atoi(os.Args[1])
	if err != nil || nodeId <= 0 {
		fmt.Fprintln(os.Stderr, "nodeId must be a positive integer")
		os.Exit(1)
	}

	clientPort := os.Args[2]

	nodeTimeout, err := strconv.Atoi(os.Args[3])
	if err != nil || nodeTimeout <= 0 {
		fmt.Fprintln(os.Stderr, "timeoutMs must be a positive integer")
		os.Exit(1)
	}

	dataDir := os.Args[4] // e.g. ./data/node1

	raftNode, err := domain.NewNode(nodeId, nodeTimeout, dataDir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to initialise node: %v\n", err)
		os.Exit(1)
	}

	handler := handlers.NewHandler(raftNode.Store, raftNode)
	router := gin.Default()
	go raftNode.Run()

	// ── RAFT RPCs ────────────────────────────────────────────────────────────
	router.POST("/appendentries", handler.HandleAppendEntries)
	router.POST("/requestvote", handler.HandleVoteRequest)

	// ── Legacy store endpoints (kept for test compatibility) ─────────────────
	router.POST("/append", handler.HandleAppendTransactionReq)
	router.POST("/getuserdetails", handler.GetUserDetails)
	router.GET("/getalluserdetails", handler.GetAllUserDetails)

	// ── Banking endpoints ────────────────────────────────────────────────────
	router.POST("/transfer", handler.HandleTransfer)
	router.POST("/balance", handler.HandleBalance)
	router.GET("/blockchain", handler.HandleGetBlockchain)

	router.Run(":" + clientPort)
}