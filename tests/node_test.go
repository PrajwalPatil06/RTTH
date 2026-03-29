package test

import (
	"RTTH/internal/domain"
	"RTTH/internal/persist"
	"RTTH/internal/structs"
	"os"
	"testing"
)

// TC_0009 — NewNode sets correct initial volatile state; timeout is randomized in [base, 2*base)
func TestNewNode_Initialization(t *testing.T) {
	node, err := domain.NewNode(1, 150, t.TempDir())
	if err != nil {
		t.Fatalf("NewNode: unexpected error: %v", err)
	}

	if node.Id != 1 {
		t.Errorf("Id: want 1, got %d", node.Id)
	}
	if node.State != "Follower" {
		t.Errorf("State: want 'Follower', got '%s'", node.State)
	}
	// Timeout is randomized to [base, 2*base) — exact value is not predictable
	if node.Timeout < 150 || node.Timeout >= 300 {
		t.Errorf("Timeout: want in [150, 300), got %d", node.Timeout)
	}
	if node.CurrentTerm != 0 {
		t.Errorf("CurrentTerm: want 0 on first boot, got %d", node.CurrentTerm)
	}
	// LeaderId=0 means unknown — must not be hardcoded to any specific node
	if node.LeaderId != 0 {
		t.Errorf("LeaderId: want 0 (unknown) on startup, got %d", node.LeaderId)
	}
	// CommitIndex and LastApplied are volatile — always 0 on startup
	if node.CommitIndex != 0 {
		t.Errorf("CommitIndex: want 0, got %d", node.CommitIndex)
	}
	if node.LastApplied != 0 {
		t.Errorf("LastApplied: want 0, got %d", node.LastApplied)
	}
	// OtherNodes contains all 3 peers before Run() removes self
	if len(node.OtherNodes) != 3 {
		t.Errorf("OtherNodes: want 3 before Run(), got %d", len(node.OtherNodes))
	}
	if node.VotedFor == nil {
		t.Error("VotedFor: must not be nil")
	}
	if node.Log == nil {
		t.Error("Log: must not be nil")
	}
}

// TC_0025 — NewNode restores CurrentTerm, VotedFor, and Log from a prior state file
func TestNewNode_LoadsPersistedState(t *testing.T) {
	dir := t.TempDir()

	// Write a pre-crash state (as if the previous run had persisted before dying)
	s, err := persist.NewStorage(dir, 1)
	if err != nil {
		t.Fatalf("NewStorage: %v", err)
	}
	if err := s.Save(persist.State{
		CurrentTerm: 4,
		VotedFor:    map[int]int{4: 2},
		Log: []structs.Transaction{
			{ID: 1, ClientID: 10, Payload: "A->B 50", Timestamp: 1000},
		},
	}); err != nil {
		t.Fatalf("Save: %v", err)
	}

	node, err := domain.NewNode(1, 150, dir)
	if err != nil {
		t.Fatalf("NewNode after simulated crash: %v", err)
	}

	if node.CurrentTerm != 4 {
		t.Errorf("CurrentTerm: want 4, got %d", node.CurrentTerm)
	}
	if node.VotedFor[4] != 2 {
		t.Errorf("VotedFor[4]: want 2, got %d", node.VotedFor[4])
	}
	if len(node.Log) != 1 || node.Log[0].Payload != "A->B 50" {
		t.Errorf("Log: unexpected value %+v", node.Log)
	}
	// Volatile fields must always restart clean
	if node.State != "Follower" {
		t.Errorf("State: must be 'Follower' after recovery, got '%s'", node.State)
	}
	if node.CommitIndex != 0 {
		t.Errorf("CommitIndex: must be 0 after recovery, got %d", node.CommitIndex)
	}
	if node.LeaderId != 0 {
		t.Errorf("LeaderId: must be 0 (unknown) after recovery, got %d", node.LeaderId)
	}
}

// TC_0026 — Persist() writes all three required fields; a fresh Storage.Load confirms them
func TestNode_Persist_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	node, err := domain.NewNode(1, 150, dir)
	if err != nil {
		t.Fatalf("NewNode: %v", err)
	}

	node.Mu.Lock()
	node.CurrentTerm = 9
	node.VotedFor[9] = 3
	node.Log = append(node.Log, structs.Transaction{
		ID: 1, ClientID: 7, Payload: "X->Y 10", Timestamp: 42,
	})
	if err := node.Persist(); err != nil {
		node.Mu.Unlock()
		t.Fatalf("Persist: %v", err)
	}
	node.Mu.Unlock()

	// Independently reload using a fresh Storage to confirm the file was written
	s, _ := persist.NewStorage(dir, 1)
	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load after Persist: %v", err)
	}

	if loaded.CurrentTerm != 9 {
		t.Errorf("CurrentTerm: want 9, got %d", loaded.CurrentTerm)
	}
	if loaded.VotedFor[9] != 3 {
		t.Errorf("VotedFor[9]: want 3, got %d", loaded.VotedFor[9])
	}
	if len(loaded.Log) != 1 || loaded.Log[0].Payload != "X->Y 10" {
		t.Errorf("Log: unexpected value %+v", loaded.Log)
	}
}

// TC_0027 — NewNode returns an error if the dataDir path cannot be created
func TestNewNode_BadDataDir(t *testing.T) {
	// Create a regular file, then try to use it as a directory.
	// os.MkdirAll fails when a path component is an existing file.
	blockingFile := t.TempDir() + "/notadir"
	if err := os.WriteFile(blockingFile, []byte("x"), 0o644); err != nil {
		t.Fatalf("setup: %v", err)
	}

	_, err := domain.NewNode(1, 150, blockingFile+"/subdir")
	if err == nil {
		t.Error("expected error when dataDir path is blocked by a file, got nil")
	}
}