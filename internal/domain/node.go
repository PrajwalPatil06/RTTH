package domain

import (
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"time"
)

type Node struct {
	Id   int
	State     string
	Store *store.MemoryStore
	OtherNodes map[int]string ////map[id]url
	LeaderId int
	LastLeaderTimeStamp int64
	Timeout int
	CurrentTerm int
	VotedFor map[int]int ////map[term]id
}

func NewNode(id int, timeout int)*Node{
	return &Node{
		Id:id,
		State: "Follower",
		Store: store.NewMemoryStore(),
		OtherNodes: map[int]string{
            1: "http://localhost:8080", 
            2: "http://localhost:8081", 
            3: "http://localhost:8082",
        },
		LeaderId : 1,
		LastLeaderTimeStamp: time.Now().UnixMilli()+2000,
		Timeout: timeout,
		CurrentTerm: 0,     // Change after persistent storage implementation
		VotedFor: map[int]int{},
	}
}

func (n *Node)Run(){
	delete(n.OtherNodes,n.Id)
	if(n.LeaderId==n.Id){
		n.State="Leader"
	}
	for{ 
		switch n.State {
		case "Leader":
			time.Sleep(100*time.Millisecond)
			n.SendHeartBeat()
		case "Follower":
			time.Sleep(50*time.Millisecond)
			if(time.Now().UnixMilli() - n.LastLeaderTimeStamp > int64(n.Timeout)) {
				// start election
				log.Println("Election needs to be started")
				n.StartElection()
			}
		}
	}
}

func (nodes *Node) SendHeartBeat() {
	heartbeatMsg := structs.HeartBeat {
		LeaderID: nodes.Id,
		Heartbeat: '@',
		Timestamp: time.Now().UnixMilli(),
	}
	jsonData, err := json.Marshal(heartbeatMsg) 
	if err != nil {
		fmt.Println(err)
		return
	}
	for _, targetURL := range nodes.OtherNodes {
		go func (url string) {
			resp, err := http.Post(url + "/heartbeat", "application/json", bytes.NewBuffer(jsonData))/////replace http.post with controlled mechanism
			if err != nil {
				//fmt.Println(err)
				return 
			}
			//log.Printf("Heartbeat sent to %s at timestamp %d\n",url, heartbeatMsg.Timestamp)
			defer resp.Body.Close()
		}(targetURL)
	}
}
func (node *Node) StartElection() {
	fmt.Println("Election started")
	node.State="Candidate"
	node.CurrentTerm++
	node.VotedFor[node.CurrentTerm] = node.Id 
	votesReceived := 1 
	requiredVotes := (len(node.OtherNodes)+1)/2+1 

	voteReq := structs.VoteReq{
		Term:        node.CurrentTerm,
		CandidateID: node.Id,
	}
	jsonData, _ := json.Marshal(voteReq)

	voteCh := make(chan bool, len(node.OtherNodes))

	for _, peer := range node.OtherNodes {
		go func(url string) {
			resp, err := http.Post(url+"/requestvote", "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				log.Println("Vote request failed:", err)
				voteCh <- false
				return
			}
			defer resp.Body.Close()

			var voteResp structs.VoteResp
			json.NewDecoder(resp.Body).Decode(&voteResp)
			voteCh <- voteResp.VoteGranted
		}(peer)
	}
	for i:=0;i<len(node.OtherNodes); i++{
		granted := <-voteCh
		if granted {
			log.Println("Vote granted")
			votesReceived++
		}

		if votesReceived >= requiredVotes && node.State == "Candidate"{
			node.State = "Leader"
			node.LeaderId = node.Id
			log.Println("Quorum reached! Became Leader for term", node.CurrentTerm)
			node.SendHeartBeat()
			return
		}
	}
	if node.State == "Candidate"{
		node.State = "Follower"
		log.Println("Election failed to reach quorum.")
	}
}