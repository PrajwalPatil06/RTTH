package domain

import (
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

type Node struct {
	Id   int
	State     string
	Store *store.MemoryStore
	OtherNodes map[int]string
	LeaderId int
	LastLeaderTimeStamp int64
}

func NewNode(id int)*Node{
	return &Node{
		Id:id,
		State: "Follower",
		Store: store.NewMemoryStore(),
		OtherNodes: map[int]string{
            1: "http://172.31.69.111:8080", 
            2: "http://172.31.69.111:8081", 
            3: "http://172.31.69.111:8082",
        },
		LeaderId : 1,
	}
}

func (n *Node)Run(){
	delete(n.OtherNodes,n.Id)
	for{
		switch n.State {
		case "Leader":
			time.Sleep(100*time.Millisecond)
			n.SendHeartBeat()
		case "Follower":
			if(time.Now().UnixMilli() - n.LastLeaderTimeStamp > 100) {
				// start election
				
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
		go func () {
			resp, err := http.Post(targetURL + "/heartbeat", "application/json", bytes.NewBuffer(jsonData))
			if err != nil {
				return 
			}
			defer resp.Body.Close()
		}()
	}
}