package test

import (
	"RTTH/internal/persist"
	"RTTH/internal/structs"
	"os"
	"path/filepath"
	"testing"
)

// TC_0018 — NewStorage creates the data directory if it does not exist
func TestNewStorage_CreatesDirectory(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "nested", "data")
	_, err := persist.NewStorage(dir, 1)
	if err != nil {
		t.Fatalf("NewStorage: unexpected error: %v", err)
	}
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("expected data directory to be created, but it does not exist")
	}
}

// TC_0019 — Load on a brand-new node (no state file) returns safe zero values, not an error
func TestStorage_Load_FirstBoot(t *testing.T) {
	s, _ := persist.NewStorage(t.TempDir(), 1)

	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load on first boot: unexpected error: %v", err)
	}
	if state.CurrentTerm != 0 {
		t.Errorf("expected CurrentTerm=0 on first boot, got %d", state.CurrentTerm)
	}
	if state.VotedFor == nil {
		t.Error("VotedFor must not be nil on first boot")
	}
	if state.Log == nil {
		t.Error("Log must not be nil on first boot")
	}
	if len(state.Log) != 0 {
		t.Errorf("expected empty Log on first boot, got %d entries", len(state.Log))
	}
}

// TC_0020 — Save then Load round-trips all three required fields exactly
func TestStorage_SaveAndLoad_RoundTrip(t *testing.T) {
	s, _ := persist.NewStorage(t.TempDir(), 2)

	original := persist.State{
		CurrentTerm: 7,
		VotedFor:    map[int]int{1: 3, 2: 3, 7: 2},
		Log: []structs.Transaction{
			{ID: 1, ClientID: 10, Payload: "A->B 100", Timestamp: 111},
			{ID: 2, ClientID: 11, Payload: "C->D 200", Timestamp: 222},
		},
	}

	if err := s.Save(original); err != nil {
		t.Fatalf("Save: unexpected error: %v", err)
	}

	loaded, err := s.Load()
	if err != nil {
		t.Fatalf("Load: unexpected error: %v", err)
	}

	if loaded.CurrentTerm != original.CurrentTerm {
		t.Errorf("CurrentTerm: want %d, got %d", original.CurrentTerm, loaded.CurrentTerm)
	}
	for term, candidate := range original.VotedFor {
		if loaded.VotedFor[term] != candidate {
			t.Errorf("VotedFor[%d]: want %d, got %d", term, candidate, loaded.VotedFor[term])
		}
	}
	if len(loaded.Log) != len(original.Log) {
		t.Fatalf("Log length: want %d, got %d", len(original.Log), len(loaded.Log))
	}
	for i, want := range original.Log {
		got := loaded.Log[i]
		if got.ID != want.ID || got.ClientID != want.ClientID || got.Payload != want.Payload {
			t.Errorf("Log[%d]: want %+v, got %+v", i, want, got)
		}
	}
}

// TC_0021 — Multiple Save calls overwrite correctly; Load always returns the latest state
func TestStorage_OverwriteOnSave(t *testing.T) {
	s, _ := persist.NewStorage(t.TempDir(), 1)

	s.Save(persist.State{CurrentTerm: 1, VotedFor: map[int]int{1: 2}, Log: nil})
	s.Save(persist.State{CurrentTerm: 5, VotedFor: map[int]int{5: 3}, Log: nil})

	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.CurrentTerm != 5 {
		t.Errorf("expected latest CurrentTerm=5, got %d", state.CurrentTerm)
	}
	if state.VotedFor[5] != 3 {
		t.Errorf("expected VotedFor[5]=3, got %d", state.VotedFor[5])
	}
}

// TC_0022 — Atomic write: the .tmp file must not exist after a successful Save
func TestStorage_AtomicWrite_NoTempFileLeft(t *testing.T) {
	dir := t.TempDir()
	s, _ := persist.NewStorage(dir, 1)

	state := persist.State{
		CurrentTerm: 3,
		VotedFor:    map[int]int{3: 1},
		Log:         []structs.Transaction{},
	}
	if err := s.Save(state); err != nil {
		t.Fatalf("Save: %v", err)
	}

	// The temporary file should be gone after a successful rename
	tmpPath := filepath.Join(dir, "node_1_state.json.tmp")
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("temp file still exists after Save — atomic rename did not complete")
	}

	// The real state file must exist
	realPath := filepath.Join(dir, "node_1_state.json")
	if _, err := os.Stat(realPath); os.IsNotExist(err) {
		t.Error("state file does not exist after Save")
	}
}

// TC_0023 — Node ID is encoded in the filename; two nodes in the same dir do not conflict
func TestStorage_PerNodeFile(t *testing.T) {
	dir := t.TempDir()
	s1, _ := persist.NewStorage(dir, 1)
	s2, _ := persist.NewStorage(dir, 2)

	s1.Save(persist.State{CurrentTerm: 10, VotedFor: map[int]int{}, Log: nil})
	s2.Save(persist.State{CurrentTerm: 20, VotedFor: map[int]int{}, Log: nil})

	loaded1, _ := s1.Load()
	loaded2, _ := s2.Load()

	if loaded1.CurrentTerm != 10 {
		t.Errorf("node 1: expected term 10, got %d", loaded1.CurrentTerm)
	}
	if loaded2.CurrentTerm != 20 {
		t.Errorf("node 2: expected term 20, got %d", loaded2.CurrentTerm)
	}
}

// TC_0024 — Load guards against nil VotedFor and nil Log from an older serialisation format
func TestStorage_Load_NilMapGuard(t *testing.T) {
	dir := t.TempDir()
	// Write a state file that omits VotedFor and Log (simulating an older format)
	path := filepath.Join(dir, "node_1_state.json")
	os.WriteFile(path, []byte(`{"current_term":3}`), 0o644)

	s, _ := persist.NewStorage(dir, 1)
	state, err := s.Load()
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if state.VotedFor == nil {
		t.Error("VotedFor must not be nil after loading a state that omitted the field")
	}
	if state.Log == nil {
		t.Error("Log must not be nil after loading a state that omitted the field")
	}
}