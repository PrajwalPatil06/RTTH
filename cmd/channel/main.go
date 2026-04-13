// cmd/channel/main.go
//
// The channel acts as an HTTP reverse-proxy between RAFT nodes.  Every
// server-to-server RPC goes through it so we can simulate network partitions
// and add random latency without touching the server code.
//
// Usage:
//
//	go run cmd/channel/main.go [port]   (default port: 9000)
//
// Routing:
//
//	POST/GET  /forward/{nodeID}/{...path}
//	          -> http://localhost:808{nodeID}/{...path}
//
//	POST      /admin/partition   body: {"partition":"1,2;3"}
//	          Use semicolons to separate groups; commas to list nodes in a group.
//	          The minority group's gate is closed.  Send "" or "reset" to reopen all.
//
// Interactive stdin:
//
//	Type a partition string (e.g. "1,2;3") and press Enter, or "reset".
package main

import (
	"bufio"
	"bytes"
	"fmt"
	"io"
	"log"
	"math/rand"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
)

// nodeAddrs maps node ID -> actual listening address.
var nodeAddrs = map[int]string{
	1: "http://localhost:8081",
	2: "http://localhost:8082",
	3: "http://localhost:8083",
}

// gate tracks which nodes are reachable.  A closed gate blocks all
// messages to AND from that node.
var (
	gateMu sync.RWMutex
	gate   = map[int]bool{1: true, 2: true, 3: true}
)

// applyPartition parses a partition string like "1,2;3" and closes the
// gate for nodes in the minority partition.
func applyPartition(partition string) {
	partition = strings.TrimSpace(partition)
	gateMu.Lock()
	defer gateMu.Unlock()

	// Reset first.
	for k := range gate {
		gate[k] = true
	}
	if partition == "" || partition == "reset" {
		log.Println("[channel] partition cleared — all gates open")
		return
	}

	parts := strings.Split(partition, ";")
	type group struct{ ids []int }
	var groups []group
	for _, p := range parts {
		var g group
		for _, tok := range strings.Split(strings.TrimSpace(p), ",") {
			id, err := strconv.Atoi(strings.TrimSpace(tok))
			if err == nil {
				g.ids = append(g.ids, id)
			}
		}
		if len(g.ids) > 0 {
			groups = append(groups, g)
		}
	}
	if len(groups) < 2 {
		return
	}

	// Close the gate for the smallest group (minority partition).
	minority := 0
	for i := 1; i < len(groups); i++ {
		if len(groups[i].ids) < len(groups[minority].ids) {
			minority = i
		}
	}
	for _, id := range groups[minority].ids {
		gate[id] = false
		log.Printf("[channel] node %d isolated", id)
	}
}

// isAllowed returns true if both sender and target are in open partitions.
func isAllowed(senderID, targetID int) bool {
	gateMu.RLock()
	defer gateMu.RUnlock()
	senderOK := senderID <= 0 || gate[senderID]  // clients (id<=0) always allowed
	return senderOK && gate[targetID]
}

// randomDelay simulates network jitter (5–30 ms).
func randomDelay() {
	time.Sleep(time.Duration(5+rand.Intn(25)) * time.Millisecond)
}

// forwardHandler proxies a request to the target node.
func forwardHandler(c *gin.Context) {
	targetIDStr := c.Param("nodeID")
	targetID, err := strconv.Atoi(targetIDStr)
	if err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": "invalid node id"})
		return
	}
	targetAddr, ok := nodeAddrs[targetID]
	if !ok {
		c.JSON(http.StatusBadRequest, gin.H{"error": "unknown node"})
		return
	}

	senderIDStr := c.GetHeader("X-Sender-ID")
	senderID, _ := strconv.Atoi(senderIDStr) // 0 if header absent (client request)

	if !isAllowed(senderID, targetID) {
		log.Printf("[channel] BLOCKED %d -> %d (partition active)", senderID, targetID)
		c.Status(http.StatusServiceUnavailable)
		return
	}

	randomDelay()

	// Build the upstream URL.
	subPath := c.Param("path") // includes leading "/"
	if subPath == "" {
		subPath = "/"
	}
	targetURL := targetAddr + subPath
	if raw := c.Request.URL.RawQuery; raw != "" {
		targetURL += "?" + raw
	}

	body, err := io.ReadAll(c.Request.Body)
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "read body: " + err.Error()})
		return
	}

	proxyReq, err := http.NewRequest(c.Request.Method, targetURL, bytes.NewReader(body))
	if err != nil {
		c.JSON(http.StatusInternalServerError, gin.H{"error": "build request: " + err.Error()})
		return
	}
	// Copy headers, skip hop-by-hop ones.
	for k, vv := range c.Request.Header {
		for _, v := range vv {
			proxyReq.Header.Add(k, v)
		}
	}

	client := &http.Client{Timeout: 500 * time.Millisecond}
	resp, err := client.Do(proxyReq)
	if err != nil {
		log.Printf("[channel] upstream error %d -> %d: %v", senderID, targetID, err)
		c.Status(http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	for k, vv := range resp.Header {
		for _, v := range vv {
			c.Header(k, v)
		}
	}
	ct := resp.Header.Get("Content-Type")
	if ct == "" {
		ct = "application/json"
	}
	c.Data(resp.StatusCode, ct, respBody)
}

// adminPartitionHandler allows HTTP-based partition control.
func adminPartitionHandler(c *gin.Context) {
	var req struct {
		Partition string `json:"partition"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		c.JSON(http.StatusBadRequest, gin.H{"error": err.Error()})
		return
	}
	applyPartition(req.Partition)
	c.JSON(http.StatusOK, gin.H{"applied": req.Partition})
}

// readStdinLoop reads partition commands interactively from stdin.
func readStdinLoop() {
	scanner := bufio.NewScanner(os.Stdin)
	fmt.Println(`[channel] Partition control ready.
  Examples: "1,2;3"  isolate node 3
            "1;2,3"  isolate node 1
            "reset"  clear all partitions`)
	for scanner.Scan() {
		applyPartition(scanner.Text())
	}
}

func main() {
	port := "9000"
	if len(os.Args) > 1 {
		port = os.Args[1]
	}

	go readStdinLoop()

	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	r.Any("/forward/:nodeID/*path", forwardHandler)
	r.POST("/admin/partition", adminPartitionHandler)

	log.Printf("[channel] proxy listening on :%s  (nodes: 8081/8082/8083)", port)
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("channel: %v", err)
	}
}