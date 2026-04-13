package store_test

import (
	"RTTH/internal/store"
	"RTTH/internal/structs"
	"sync"
	"testing"
)

// TC_0002 — Append assigns a sequential index; GetByID retrieves by that index.
func TestMemoryStore_AppendAndGetByID(t *testing.T) {
	s := store.NewMemoryStore()

	txn := structs.Transaction{ClientID: 99, Payload: "A->B 10", Timestamp: 123456789}
	if err := s.Append(txn); err != nil {
		t.Fatalf("Append: unexpected error: %v", err)
	}

	got, err := s.GetByID(1)
	if err != nil {
		t.Fatalf("GetByID(1): unexpected error: %v", err)
	}
	if got.ID != 1 {
		t.Errorf("expected assigned ID=1, got %d", got.ID)
	}
	if got.ClientID != 99 || got.Payload != "A->B 10" {
		t.Errorf("unexpected payload: %+v", got)
	}
}

// TC_0003 — GetAll returns all entries as a map keyed by log index.
func TestMemoryStore_GetAll(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 1, Payload: "A->B 10"})
	s.Append(structs.Transaction{ClientID: 2, Payload: "C->D 20"})

	all := s.GetAll()
	if len(all) != 2 {
		t.Errorf("expected 2 entries, got %d", len(all))
	}
	if _, ok := all[1]; !ok {
		t.Error("expected entry at index 1")
	}
	if _, ok := all[2]; !ok {
		t.Error("expected entry at index 2")
	}
}

// TC_0004 — 100 concurrent writers and 50 concurrent readers do not race or
// corrupt state (run with -race).
func TestMemoryStore_Concurrency(t *testing.T) {
	s := store.NewMemoryStore()
	var wg sync.WaitGroup

	for i := 1; i <= 100; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			s.Append(structs.Transaction{ClientID: id, Payload: "payload", Timestamp: 123})
		}(i)
	}

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_ = s.GetAll()
		}()
	}

	wg.Wait()

	if len(s.GetAll()) != 100 {
		t.Errorf("expected 100 entries after concurrent writes, got %d", len(s.GetAll()))
	}
}

// TC_0011 — GetByID returns an explicit error for a missing key.
func TestMemoryStore_GetByID_NotFound(t *testing.T) {
	s := store.NewMemoryStore()
	_, err := s.GetByID(999)
	if err == nil {
		t.Error("expected error for missing key, got nil")
	}
}

// TC_0015 — The store auto-increments the log index; callers cannot dictate it.
func TestMemoryStore_SequentialIndexing(t *testing.T) {
	s := store.NewMemoryStore()

	for i := 0; i < 5; i++ {
		s.Append(structs.Transaction{ID: 999, ClientID: i + 1, Payload: "x"})
	}

	all := s.GetAll()
	for idx := 1; idx <= 5; idx++ {
		entry, ok := all[idx]
		if !ok {
			t.Errorf("expected entry at index %d", idx)
			continue
		}
		if entry.ID != idx {
			t.Errorf("entry at key %d has ID=%d; store must override caller-supplied ID", idx, entry.ID)
		}
	}
}

// TC_0016 — GetAll returns a copy; mutating the result does not affect the store.
func TestMemoryStore_GetAll_ReturnsCopy(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 1, Payload: "original"})

	copy1 := s.GetAll()
	copy1[1] = structs.Transaction{ClientID: 1, Payload: "mutated"}

	copy2 := s.GetAll()
	if copy2[1].Payload != "original" {
		t.Errorf("store internal state was mutated through returned map: got %q", copy2[1].Payload)
	}
}

// TC_0017 — Append on an empty store gives ID=1; subsequent entries get 2, 3 …
func TestMemoryStore_FirstIndexIsOne(t *testing.T) {
	s := store.NewMemoryStore()
	s.Append(structs.Transaction{ClientID: 5, Payload: "first"})

	got, err := s.GetByID(1)
	if err != nil {
		t.Fatalf("expected entry at ID=1, got error: %v", err)
	}
	if got.ClientID != 5 {
		t.Errorf("expected ClientID=5, got %d", got.ClientID)
	}
}