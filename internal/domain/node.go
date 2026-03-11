package domain

import (
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"sync"
	"time"
)

type Node struct {
	Mu sync.Mutex

	Id                  int
	State               string
	Store               *store.MemoryStore
	OtherNodes          map[int]string ////map[id]url
	LeaderId            int
	LastLeaderTimeStamp int64
	Timeout             int
	CurrentTerm         int
	VotedFor            map[int]int ////map[term]id
}

func NewNode(id int, timeout int) *Node {
	return &Node{
		Id:    id,
		State: "Follower",
		Store: store.NewMemoryStore(),
		OtherNodes: map[int]string{
			1: "http://localhost:8080",
			2: "http://localhost:8081",
			3: "http://localhost:8082",
		},
		LeaderId:            1,
		LastLeaderTimeStamp: time.Now().UnixMilli() + 2000,
		Timeout:             timeout,
		CurrentTerm:         0, // Change after persistent storage implementation
		VotedFor:            map[int]int{},
	}
}

// RACE COND 1 (ADD MUTEX LOCK)
func (n *Node) Run() {

	n.Mu.Lock()
	delete(n.OtherNodes, n.Id)
	if n.LeaderId == n.Id {
		n.State = "Leader"
	}
	n.Mu.Unlock()

	for {
		n.Mu.Lock()
		state := n.State
		lastLeader := n.LastLeaderTimeStamp
		timeout := n.Timeout
		n.Mu.Unlock()

		switch state {
		case "Leader":
			time.Sleep(100 * time.Millisecond)
			n.SendHeartBeat()
		case "Follower":
			time.Sleep(50 * time.Millisecond)
			if time.Now().UnixMilli()-lastLeader > int64(timeout) {
				// start election
				log.Println("Election needs to be started")
				n.StartElection()
			}
		}
	}
}

// major change
func (nodes *Node) SendHeartBeat() {

	nodes.Mu.Lock()
	id := nodes.Id
	var peerURLs []string
	for _, url := range nodes.OtherNodes {
		peerURLs = append(peerURLs, url)
	}

	nodes.Mu.Unlock()

	heartbeatMsg := structs.HeartBeat{
		LeaderID:  id,
		Heartbeat: '@',
		Timestamp: time.Now().UnixMilli(),
	}
	jsonData, err := json.Marshal(heartbeatMsg)
	if err != nil {
		fmt.Println(err)
		return
	}
	// timeout
	client := &http.Client{Timeout: 150 * time.Millisecond}

	for _, targetURL := range peerURLs {
		go func(url string) {
			resp, err := client.Post(url+"/heartbeat", "application/json", bytes.NewBuffer(jsonData)) /////replace http.post with controlled mechanism
			if err != nil {
				//fmt.Println(err)
				return
			}
			//log.Printf("Heartbeat sent to %s at timestamp %d\n",url, heartbeatMsg.Timestamp)
			defer resp.Body.Close()
		}(targetURL)
	}
}

// major change
func (node *Node) StartElection() {

	node.Mu.Lock()

	fmt.Println("Election started")
	node.State = "Candidate"
	node.CurrentTerm++
	node.LastLeaderTimeStamp = time.Now().UnixMilli()
	node.VotedFor[node.CurrentTerm] = node.Id

	term := node.CurrentTerm
	id := node.Id
	var peerURLs []string
	for _, url := range node.OtherNodes {
		peerURLs = append(peerURLs, url)
	}
	requiredVotes := (len(node.OtherNodes)+1)/2 + 1

	node.Mu.Unlock()
	votesReceived := 1

	voteReq := structs.VoteReq{
		Term:        term,
		CandidateID: id,
	}
	jsonData, _ := json.Marshal(voteReq)

	voteCh := make(chan bool, len(peerURLs))

	// add timeout
	client := &http.Client{Timeout: 200 * time.Millisecond}

	for _, peer := range peerURLs {
		go func(url string) {
			resp, err := client.Post(url+"/requestvote", "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				log.Println("Vote request failed:", err)
				voteCh <- false
				return
			}
			defer resp.Body.Close()

			var voteResp structs.VoteResp
			if err := json.NewDecoder(resp.Body).Decode(&voteResp); err != nil {
				log.Println("Failed to decode vote response:", err)
				voteCh <- false
				return
			}
			voteCh <- voteResp.VoteGranted
		}(peer)
	}
	for i := 0; i < len(peerURLs); i++ {
		granted := <-voteCh
		if granted {
			log.Println("Vote granted")
			votesReceived++
		}

		node.Mu.Lock()
		if votesReceived >= requiredVotes && node.State == "Candidate" {
			node.State = "Leader"
			node.LeaderId = node.Id
			node.Mu.Unlock()

			log.Println("Quorum reached! Became Leader for term", term)
			node.SendHeartBeat()
			return
		}
		node.Mu.Unlock()
	}
	node.Mu.Lock()
	if node.State == "Candidate" {
		node.State = "Follower"
	}
	node.Mu.Unlock()
	log.Println("Election failed to reach quorum.")
}
