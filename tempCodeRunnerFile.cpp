#define _WIN32_WINNT 0x0601 // Targets Windows 7 or higher
#pragma comment(lib, "ws2_32.lib")
#include<iostream>
#include<fstream>
#include<vector>
#include<set>
#include<string>
#include<map>
#include "mingw-std-threads/mingw.mutex.h"
#include<chrono>
#include"mingw-std-threads/mingw.thread.h"
#include<winsock2.h>
#include<ws2tcpip.h>
#include<sstream>



using namespace std;

struct Transaction{
    int id;
    string from;
    string to;
    double amount;
};

struct LogEntry{
    int term;
    Transaction tx;
};

enum Role{Follower,Leader,Candidate};

class stable_storage{
    string filePath;
    public:
    stable_storage(int nodeID){
        filePath="node_"+ to_string(nodeID)+"_storage.txt";
    }
    void save(int term,int votedFor,int commitLength,const vector<LogEntry> &log){
        ofstream out(filePath,ios::trunc);
        out << term << " " << votedFor << " " << commitLength << endl;
        out<<log.size()<<endl;
        for(const auto &entry : log){
            out<<entry.term<< " " << entry.tx.amount<<" "<<entry.tx.from<< " "<<entry.tx.to<<" "<<entry.tx.id<<endl;
        }
        
        out.close();
    }
    bool load(int& term,int& votedFor,int& commitLength,vector<LogEntry>&log){
        ifstream in(filePath);
        if(!in.is_open()) return false;
        int logSize;
        if (!(in >> term >> votedFor >> commitLength >> logSize)) return false;

        log.clear();
        for (int i = 0; i < logSize; ++i) {
            LogEntry entry;
            in >> entry.term >> entry.tx.amount >> entry.tx.from >> entry.tx.to >> entry.tx.id;
            log.push_back(entry);
        }
        return true;
    }
};

class RaftNode;

class NetworkManager{
    int port;
    SOCKET server_fd;
    map<int,int> peers;

    public:
    NetworkManager(int p,map<int,int> clusterPeers):port(p),peers(clusterPeers){
        WSADATA wsadata;
        if(WSAStartup(MAKEWORD(2,2),&wsadata)!=0){
            cerr<<"Winsock Initialization failed"<<endl;
        }
    }
    ~NetworkManager(){
        closesocket(server_fd);
        WSACleanup();
    }

    void sendToPeer(int peerID,string message){
        if(peers.find(peerID)==peers.end()){
            return;
        }
        SOCKET sock = socket(AF_INET,SOCK_STREAM,0);
        if(sock==INVALID_SOCKET) return;

        sockaddr_in serv_addr;
        serv_addr.sin_family = AF_INET;
        serv_addr.sin_port = htons(peers[peerID]);
        serv_addr.sin_addr.s_addr = inet_addr("127.0.0.1");

        DWORD timeout=100;
        setsockopt(sock,SOL_SOCKET,SO_SNDTIMEO,(char*)&timeout, sizeof(timeout));

        if (connect(sock, (struct sockaddr*)&serv_addr,sizeof(serv_addr)) != SOCKET_ERROR){
            send(sock,message.c_str(),(int)message.length(),0);
        }

        closesocket(sock);
    }

    void broadcast(string message,int selfID){
        for(auto const& x : peers){
            int peerID = x.first;
            int peerPort = x.second;
            if(peerID==selfID){
                continue;
            }
            thread([this,peerID,message](){
                this->sendToPeer(peerID,message);
            }).detach();
        }
    }

};
vector<string> split(const string& s,char delimiter){
    vector<string> tokens;
    string token;
    istringstream tokenStream(s);
    while(getline(tokenStream,token,delimiter)){
        tokens.push_back(token);
    }
    return tokens;
}
class RaftNode{
    private:
    int currentTerm;
    int votedFor;
    vector<LogEntry> log;
    int commitLength;

    Role currentRole;
    int currentLeader;
    set<int> votesRecieved;

    stable_storage storage;
    int nodeID;
    int last_applied;
    map<string,double> accounts;

    mutex raftMutex;
    chrono::steady_clock::time_point lastContact;
    int electionTimeout;
    NetworkManager nm;
    int clusterSize;

    public:

    RaftNode(int id,map<int,int> &peerPorts): nodeID(id),storage(id),last_applied(0),nm(peerPorts[id],peerPorts),clusterSize(peerPorts.size()) {
        currentRole = Follower;
        currentLeader=-1;
        electionTimeout=150+(rand()%150);

        if(!storage.load(currentTerm,votedFor,commitLength,log)){
            currentTerm=0;
            votedFor=-1;
            commitLength=0;
        }
        accounts["Alice"] = 1000.0;
        accounts["Bob"] = 1000.0;
        thread(&RaftNode::runTimer,this).detach();
    }
    void persist(){
        storage.save(currentTerm,votedFor,commitLength,log);
    }
    void runTimer(){
        while(true){
            this_thread::sleep_for(chrono::milliseconds(10));
            lock_guard<mutex> lock(raftMutex);
            auto now=chrono::steady_clock::now();
            auto duration =chrono::duration_cast<chrono::milliseconds>(now-lastContact).count();

            if(currentRole != Leader && duration >electionTimeout){
                startElection();
            }
            if(currentRole==Leader){
                sendHeartbeats();
            }
        }
    }
    void sendHeartbeats() {
        // Serialization for Heartbeat
        string msg = "HEARTBEAT|" + to_string(nodeID) + "|" + 
                     to_string(currentTerm) + "|" + to_string(commitLength);
        
        // Parallel send via NetworkManager
        nm.broadcast(msg, nodeID);
    }


    void startElection() {
        currentRole = Candidate;
        currentTerm++;
        votedFor = nodeID;
        votesRecieved.clear();
        votesRecieved.insert(nodeID); 

        lastContact = chrono::steady_clock::now();
        persist();
        cout << "Node " << nodeID << " became Candidate for Term " << currentTerm << endl;
        int lastLogIndex = (int)log.size() - 1;
        int lastLogTerm = 0;
        if (lastLogIndex >= 0) {
            lastLogTerm = log[lastLogIndex].term;
        }
        string msg = "VOTE_REQ|" + to_string(nodeID) + "|" + 
                 to_string(currentTerm) + "|" + 
                 to_string(lastLogIndex) + "|" + 
                 to_string(lastLogTerm);
        nm.broadcast(msg, nodeID);
    }

    void onClientRequest(Transaction tx){
        lock_guard<mutex> lock(raftMutex);
        if(currentRole!=Leader){
            return;
        }
        log.push_back({currentTerm,tx});
        persist();
    }

    void applyToStateMachine(){
        while(last_applied < commitLength){
            Transaction tx=log[last_applied].tx;
            if(accounts[tx.from]>=tx.amount){
                accounts[tx.from]-=tx.amount;
                accounts[tx.to]+=tx.amount;
            }
            last_applied++;
        }
        
    }

    void onReceiveHeartbeat(int leaderID,int term,int leaderCommit){
        lock_guard<mutex> lock(raftMutex);
        if(term>=currentTerm){
            currentTerm=term;
            currentRole=Follower;
            currentLeader=leaderID;
            lastContact=chrono::steady_clock::now();

            if(leaderCommit >commitLength){
                commitLength=min(leaderCommit,(int)log.size());
                applyToStateMachine();
            }
        }
    }
    void handleIncomingMessage(string message) {
        lock_guard<mutex> lock(raftMutex);
        vector<string> parts = split(message, '|');
        if (parts.empty()) return;

        string type = parts[0];

        if (type == "HEARTBEAT") {
            // Format: HEARTBEAT|leaderID|term|leaderCommit
            int lID = stoi(parts[1]);
            int trm = stoi(parts[2]);
            int cLen = stoi(parts[3]);
            // Call logic: onReceiveHeartbeat(lID, trm, cLen);
        }
        // Inside handleIncomingMessage...
        else if (type == "VOTE_REQ") {
    // Parse: VOTE_REQ | cID | cTerm | cLogIdx | cLogTerm
            int cID = stoi(parts[1]);
            int cTerm = stoi(parts[2]);
            int cLogIdx = stoi(parts[3]);
            int cLogTerm = stoi(parts[4]);

    // 1. Term Check: If candidate's term is older, reject immediately.
            if (cTerm < currentTerm) {
        // Optional: Send a reject message (Phase 4), for now just ignore
                return; 
            }

    // 2. Step Down: If we see a higher term, update ourselves to Follower
            if (cTerm > currentTerm) {
                currentTerm = cTerm;
                currentRole = Follower;
                votedFor = -1; // Reset vote for new term
                persist();
            }

    // 3. Log Freshness Check (Election Safety)
    // "Is the candidate's log at least as up-to-date as mine?"
            int myLogIdx = (int)log.size() - 1;
            int myLogTerm = (myLogIdx >= 0) ? log[myLogIdx].term : 0;

            bool logIsOk = (cLogTerm > myLogTerm) || 
                   (cLogTerm == myLogTerm && cLogIdx >= myLogIdx);

    // 4. The Voting Logic
            if (logIsOk && (votedFor == -1 || votedFor == cID)) {
        // GRANT VOTE
                votedFor = cID;
                persist();
                lastContact = chrono::steady_clock::now(); // Reset timer so we don't start our own election
        
                cout << "Voted FOR Node " << cID << " in Term " << currentTerm << endl;

        // Send 'VOTE_ACK' back to the candidate
                string voteMsg = "VOTE_ACK|" + to_string(nodeID) + "|" + 
                         to_string(currentTerm);
                nm.sendToPeer(cID, voteMsg);
            }
        }
        else if (type == "VOTE_ACK") {
    // Format: VOTE_ACK | VoterID | Term
            int voterID = stoi(parts[1]);
            int vTerm = stoi(parts[2]);

    // Only count votes if I am still a Candidate and the term matches
            if (currentRole == Candidate && vTerm == currentTerm) {
                votesRecieved.insert(voterID);
                cout << "Node " << nodeID << " received vote from " << voterID << endl;

        // CHECK FOR MAJORITY VICTORY
        // Majority = (Total Nodes / 2) + 1
        // For a 3-node cluster, we need > 1.5 (so 2 votes)
                int clusterSize = 3; // Or pass this variable into your class
        
                if (votesRecieved.size() > clusterSize / 2) {
                    becomeLeader();
                }
            }
        }
    }

    void becomeLeader() {
        currentRole = Leader;
        currentLeader = nodeID;
        cout << "!!! Node " << nodeID << " won the election! Becoming LEADER for Term " << currentTerm << " !!!" << endl;
    // IMPORTANT: Reset nextIndex/matchIndex for followers (Phase 5 stuff)
    // For now, just sending heartbeats immediately is enough to assert dominance.
    
        sendHeartbeats(); // Assert authority immediately
    }
    void startServer(RaftNode* node) {
        server_fd = socket(AF_INET, SOCK_STREAM, 0);
        sockaddr_in address;
        address.sin_family = AF_INET;
        address.sin_addr.s_addr = INADDR_ANY;
        address.sin_port = htons(port); // Listens on its own specific port

        if (bind(server_fd, (struct sockaddr*)&address, sizeof(address)) == SOCKET_ERROR) {
            cerr << "Bind failed for port " << port << endl;
            return;
        }
        listen(server_fd, 3);

        while (true) {
            SOCKET new_socket = accept(server_fd, NULL, NULL);
            if (new_socket != INVALID_SOCKET) {
                // Handle the incoming message in a separate thread
                thread([new_socket, node]() {
                    char buffer[1024] = {0};
                    int valread = recv(new_socket, buffer, 1024, 0);
                    if (valread > 0) {
                        node->handleIncomingMessage(string(buffer));
                    }
                    closesocket(new_socket);
                }).detach();
            }
        }
    }
    
};





int main() {
    // 1. Define the Cluster Configuration (The "Phonebook")
    map<int, int> peerPorts;
    peerPorts[0] = 8001; // Node 0 lives at Port 8001
    peerPorts[1] = 8002; // Node 1 lives at Port 8002
    peerPorts[2] = 8003; // Node 2 lives at Port 8003

    // 2. Ask the user who they are
    int myID;
    cout << "Enter Node ID (0, 1, or 2): ";
    cin >> myID;

    // 3. Validation
    if (peerPorts.find(myID) == peerPorts.end()) {
        cerr << "Invalid ID! Must be 0, 1, or 2." << endl;
        return -1;
    }

    cout << "Starting Node " << myID << " on Port " << peerPorts[myID] << "..." << endl;

    // 4. Create the Node (This starts the Timers and the Server)
    RaftNode node(myID, peerPorts);

    // 5. Keep the Main Thread alive so the background threads can work
    while (true) {
        // Just sleep to save CPU. The RaftNode threads are doing the real work.
        this_thread::sleep_for(chrono::seconds(1));
    }

    return 0;
}