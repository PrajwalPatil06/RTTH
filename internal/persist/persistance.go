package persist

import (
	"RTTH/internal/structs"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// State is everything that must survive a node crash.
type State struct {
	CurrentTerm int                   `json:"current_term"`
	VotedFor    map[int]int           `json:"voted_for"`    // map[term]candidateId
	Log         []structs.Transaction `json:"log"`
	Blockchain  []structs.Block       `json:"blockchain"`   // mined, committed blocks
	BlockBuffer []structs.Transaction `json:"block_buffer"` // committed txns waiting to fill next block
}

// Storage wraps the per-node state file.
type Storage struct {
	path string
}

// NewStorage creates the dataDir if required and returns a Storage bound to
// the node-specific file inside it.
func NewStorage(dataDir string, nodeId int) (*Storage, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("persist: cannot create data dir %s: %w", dataDir, err)
	}
	return &Storage{
		path: filepath.Join(dataDir, fmt.Sprintf("node_%d_state.json", nodeId)),
	}, nil
}

// Save atomically overwrites the state file via a temp-file + rename.
func (s *Storage) Save(state State) error {
	data, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("persist: marshal failed: %w", err)
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0o644); err != nil {
		return fmt.Errorf("persist: write temp file failed: %w", err)
	}
	if err := os.Rename(tmp, s.path); err != nil {
		return fmt.Errorf("persist: rename failed: %w", err)
	}
	return nil
}

// Load reads and deserialises the state file.  On first boot (no file) it
// returns safe zero values.  Nil maps and slices are always initialised to
// empty non-nil values so callers need no extra nil-guards.
func (s *Storage) Load() (State, error) {
	data, err := os.ReadFile(s.path)
	if os.IsNotExist(err) {
		return State{
			VotedFor:    map[int]int{},
			Log:         []structs.Transaction{},
			Blockchain:  []structs.Block{},
			BlockBuffer: []structs.Transaction{},
		}, nil
	}
	if err != nil {
		return State{}, fmt.Errorf("persist: read failed: %w", err)
	}

	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return State{}, fmt.Errorf("persist: unmarshal failed: %w", err)
	}
	// Guard against older serialisation formats that omitted these fields.
	if state.VotedFor == nil {
		state.VotedFor = map[int]int{}
	}
	if state.Log == nil {
		state.Log = []structs.Transaction{}
	}
	if state.Blockchain == nil {
		state.Blockchain = []structs.Block{}
	}
	if state.BlockBuffer == nil {
		state.BlockBuffer = []structs.Transaction{}
	}
	return state, nil
}