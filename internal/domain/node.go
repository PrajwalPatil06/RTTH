package domain

import (
	"RTTH/internal/persist"
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"log"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

type Node struct {
	Mu sync.Mutex

	Id                  int
	State               string
	Store               *store.MemoryStore
	OtherNodes          map[int]string
	LeaderId            int
	LastLeaderTimeStamp int64
	Timeout             int

	CurrentTerm int
	VotedFor    map[int]int           
	Log         []structs.Transaction 

	CommitIndex int 
	LastApplied int 

	storage *persist.Storage
}

func NewNode(id int, timeout int, dataDir string) (*Node, error) {
	randomizedTimeout := timeout + rand.Intn(timeout)

	storage, err := persist.NewStorage(dataDir, id)
	if err != nil {
		return nil, err
	}

	saved, err := storage.Load()
	if err != nil {
		return nil, err
	}

	n := &Node{
		Id:    id,
		State: "Follower", 
		Store: store.NewMemoryStore(),
		OtherNodes: map[int]string{
			1: "http://localhost:8080",
			2: "http://localhost:8081",
			3: "http://localhost:8082",
		},
		LeaderId:            0, 
		LastLeaderTimeStamp: time.Now().UnixMilli() + int64(randomizedTimeout),
		Timeout:             randomizedTimeout,

		CurrentTerm: saved.CurrentTerm,
		VotedFor:    saved.VotedFor,
		Log:         saved.Log,

		CommitIndex: 0,
		LastApplied: 0,

		storage: storage,
	}
	return n, nil
}

func (n *Node) Persist() error {
	return n.storage.Save(persist.State{
		CurrentTerm: n.CurrentTerm,
		VotedFor:    n.VotedFor,
		Log:         n.Log,
	})
}

func (n *Node) Run() {
	n.Mu.Lock()
	delete(n.OtherNodes, n.Id)
	n.Mu.Unlock()

	for {
		n.Mu.Lock()
		state := n.State
		lastLeader := n.LastLeaderTimeStamp
		timeout := n.Timeout
		n.Mu.Unlock()

		switch state {
		case "Leader":
			interval := time.Duration(timeout/5) * time.Millisecond
			time.Sleep(interval)
			n.SendHeartBeat()
		case "Follower":
			time.Sleep(50 * time.Millisecond)
			if time.Now().UnixMilli()-lastLeader > int64(timeout) {
				log.Println("Election timeout — starting election")
				n.StartElection()
			}
		case "Candidate":
			time.Sleep(50 * time.Millisecond)
		}
	}
}

func (n *Node) SendHeartBeat() {
	n.Mu.Lock()
	id := n.Id
	term := n.CurrentTerm
	commitIndex := n.CommitIndex
	var peerURLs []string
	for _, url := range n.OtherNodes {
		peerURLs = append(peerURLs, url)
	}
	n.Mu.Unlock()

	req := structs.AppendEntriesReq{
		Term:         term,
		LeaderID:     id,
		Entries:      []structs.Transaction{},
		LeaderCommit: commitIndex,
	}
	jsonData, err := json.Marshal(req)
	if err != nil {
		log.Println("Failed to marshal AppendEntries:", err)
		return
	}

	client := &http.Client{Timeout: 150 * time.Millisecond}

	for _, targetURL := range peerURLs {
		go func(url string) {
			resp, err := client.Post(url+"/appendentries", "application/json", bytes.NewReader(jsonData))
			if err != nil {
				return
			}
			defer resp.Body.Close()

			var aeResp structs.AppendEntriesResp
			if err := json.NewDecoder(resp.Body).Decode(&aeResp); err != nil {
				return
			}

			n.Mu.Lock()
			defer n.Mu.Unlock()
			if aeResp.Term > n.CurrentTerm {
				n.CurrentTerm = aeResp.Term
				n.State = "Follower"
				n.LeaderId = 0
				if err := n.Persist(); err != nil {
					log.Println("persist failed after stepping down:", err)
				}
			}
		}(targetURL)
	}
}

func (n *Node) StartElection() {
	n.Mu.Lock()
	log.Println("Election started")
	n.State = "Candidate"
	n.CurrentTerm++
	n.LastLeaderTimeStamp = time.Now().UnixMilli()
	n.VotedFor[n.CurrentTerm] = n.Id

	if err := n.Persist(); err != nil {
		log.Println("persist failed at election start — aborting:", err)
		n.State = "Follower"
		n.Mu.Unlock()
		return
	}

	term := n.CurrentTerm
	id := n.Id
	var peerURLs []string
	for _, url := range n.OtherNodes {
		peerURLs = append(peerURLs, url)
	}
	requiredVotes := (len(n.OtherNodes)+1)/2 + 1
	n.Mu.Unlock()

	votesReceived := 1

	voteReq := structs.VoteReq{
		Term:        term,
		CandidateID: id,
		Timestamp:   time.Now().UnixMilli(),
	}
	jsonData, err := json.Marshal(voteReq)
	if err != nil {
		log.Println("Failed to marshal vote request:", err)
		n.Mu.Lock()
		if n.State == "Candidate" {
			n.State = "Follower"
		}
		n.Mu.Unlock()
		return
	}

	voteCh := make(chan bool, len(peerURLs))
	client := &http.Client{Timeout: 200 * time.Millisecond}

	for _, peer := range peerURLs {
		go func(url string) {
			resp, err := client.Post(url+"/requestvote", "application/json", bytes.NewReader(jsonData))
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

			n.Mu.Lock()
			if voteResp.Term > n.CurrentTerm {
				n.CurrentTerm = voteResp.Term
				n.State = "Follower"
				n.LeaderId = 0
				if err := n.Persist(); err != nil {
					log.Println("persist failed on term update from vote response:", err)
				}
			}
			n.Mu.Unlock()

			voteCh <- voteResp.VoteGranted
		}(peer)
	}

	for i := 0; i < len(peerURLs); i++ {
		granted := <-voteCh
		if granted {
			votesReceived++
		}

		n.Mu.Lock()
		if n.State != "Candidate" {
			n.Mu.Unlock()
			return
		}
		if votesReceived >= requiredVotes {
			n.State = "Leader"
			n.LeaderId = n.Id
			n.Mu.Unlock()
			log.Println("Quorum reached! Became Leader for term", term)
			n.SendHeartBeat()
			return
		}
		n.Mu.Unlock()
	}

	n.Mu.Lock()
	if n.State == "Candidate" {
		n.State = "Follower"
	}
	n.Mu.Unlock()
	log.Println("Election failed to reach quorum.")
}