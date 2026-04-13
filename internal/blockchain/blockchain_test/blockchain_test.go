package blockchain_test

import (
	"RTTH/internal/blockchain"
	"RTTH/internal/structs"
	"crypto/sha256"
	"fmt"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// helper: returns the directory where this test file lives.
func testdataDir(t *testing.T) string {
	t.Helper()
	_, file, _, _ := runtime.Caller(0)
	// Walk up two levels: internal/blockchain → project root, then test/testdata.
	return filepath.Join(filepath.Dir(file), "..", "..", "test", "testdata")
}

// ── Proof-of-Work ─────────────────────────────────────────────────────────────

// TC_0066 — BuildBlock produces a block whose hash ends in '0', '1', or '2'.
func TestBuildBlock_ValidHash(t *testing.T) {
	txns := []structs.Transaction{
		{ID: 1, ClientID: 1, Payload: "2 100", Term: 1},
		{ID: 2, ClientID: 2, Payload: "3 50", Term: 1},
		{ID: 3, ClientID: 3, Payload: "1 10", Term: 1},
	}
	block := blockchain.BuildBlock(nil, txns, 1)

	if block.Hash == "" {
		t.Fatal("BuildBlock returned empty hash")
	}
	last := block.Hash[len(block.Hash)-1]
	if last != '0' && last != '1' && last != '2' {
		t.Errorf("PoW invariant violated: hash must end in 0/1/2, got %q (hash=%s)", last, block.Hash)
	}
}

// TC_0067 — BuildBlock sets PrevHash to "0" for the genesis block.
func TestBuildBlock_GenesisBlockPrevHash(t *testing.T) {
	txns := []structs.Transaction{{ID: 1, ClientID: 1, Payload: "2 10", Term: 0}}
	block := blockchain.BuildBlock(nil, txns, 0)

	if block.PrevHash != "0" {
		t.Errorf("genesis PrevHash: want '0', got %q", block.PrevHash)
	}
}

// TC_0068 — Each block's PrevHash equals the Hash of the block before it.
func TestBuildBlock_ChainLinksCorrectly(t *testing.T) {
	txns1 := []structs.Transaction{
		{ID: 1, ClientID: 1, Payload: "2 10", Term: 1},
		{ID: 2, ClientID: 2, Payload: "3 20", Term: 1},
		{ID: 3, ClientID: 3, Payload: "1 5", Term: 1},
	}
	txns2 := []structs.Transaction{
		{ID: 4, ClientID: 1, Payload: "3 15", Term: 2},
		{ID: 5, ClientID: 2, Payload: "1 25", Term: 2},
		{ID: 6, ClientID: 3, Payload: "2 35", Term: 2},
	}

	var chain []structs.Block
	b1 := blockchain.BuildBlock(chain, txns1, 1)
	chain = append(chain, b1)
	b2 := blockchain.BuildBlock(chain, txns2, 2)

	if b2.PrevHash != b1.Hash {
		t.Errorf("b2.PrevHash=%q, want b1.Hash=%q", b2.PrevHash, b1.Hash)
	}
}

// TC_0069 — BuildBlock stores a copy of the transaction slice; mutating the
// input after the fact does not alter the mined block.
func TestBuildBlock_StoresTxnCopy(t *testing.T) {
	txns := []structs.Transaction{
		{ID: 1, ClientID: 1, Payload: "original", Term: 1},
		{ID: 2, ClientID: 2, Payload: "original", Term: 1},
		{ID: 3, ClientID: 3, Payload: "original", Term: 1},
	}
	block := blockchain.BuildBlock(nil, txns, 1)

	// Mutate after mining.
	txns[0].Payload = "mutated"

	if block.Txns[0].Payload != "original" {
		t.Error("BuildBlock must store a copy; block was mutated via the input slice")
	}
}

// TC_0070 — Nonce is non-empty and produces the claimed hash when recomputed.
func TestBuildBlock_HashIsReproducible(t *testing.T) {
	txns := []structs.Transaction{
		{ID: 1, ClientID: 1, Payload: "2 10", Term: 1},
		{ID: 2, ClientID: 2, Payload: "3 20", Term: 1},
		{ID: 3, ClientID: 3, Payload: "1 5", Term: 1},
	}
	block := blockchain.BuildBlock(nil, txns, 1)

	if block.Nonce == "" {
		t.Fatal("Nonce must not be empty")
	}

	// Reproduce the hash using the same algorithm that blockchain.go uses.
	var sb strings.Builder
	sb.WriteString(block.PrevHash)
	sb.WriteString(block.Nonce)
	for _, tx := range block.Txns {
		fmt.Fprintf(&sb, "%d%d%s%d%d", tx.ID, tx.ClientID, tx.Payload, tx.Timestamp, tx.Term)
	}
	h := sha256.Sum256([]byte(sb.String()))
	want := fmt.Sprintf("%x", h)

	if block.Hash != want {
		t.Errorf("Hash mismatch: stored=%q, recomputed=%q", block.Hash, want)
	}
}

// ── Balance calculation ───────────────────────────────────────────────────────

// TC_0071 — GetCommittedBalance on an empty chain returns 0.
func TestGetCommittedBalance_EmptyChain(t *testing.T) {
	bal := blockchain.GetCommittedBalance(nil, 1)
	if bal != 0 {
		t.Errorf("want 0 for empty chain, got %d", bal)
	}
}

// TC_0072 — Genesis credit (sender=0) increases the receiver's balance.
func TestGetCommittedBalance_GenesisCredit(t *testing.T) {
	txns := []structs.Transaction{
		{ClientID: 0, Payload: "1 1000", Term: 0}, // bank → client 1
		{ClientID: 0, Payload: "2 1000", Term: 0}, // bank → client 2
		{ClientID: 0, Payload: "3 1000", Term: 0}, // bank → client 3
	}
	block := blockchain.BuildBlock(nil, txns, 0)
	chain := []structs.Block{block}

	if bal := blockchain.GetCommittedBalance(chain, 1); bal != 1000 {
		t.Errorf("client 1 balance: want 1000, got %d", bal)
	}
	if bal := blockchain.GetCommittedBalance(chain, 2); bal != 1000 {
		t.Errorf("client 2 balance: want 1000, got %d", bal)
	}
	// Genesis sender (0) must not be debited.
	if bal := blockchain.GetCommittedBalance(chain, 0); bal != 0 {
		t.Errorf("genesis sender balance: want 0 (never debited), got %d", bal)
	}
}

// TC_0073 — Transfer from one client to another adjusts both balances correctly.
func TestGetCommittedBalance_Transfer(t *testing.T) {
	// Seed: clients 1 & 2 each get 500.
	seed := []structs.Transaction{
		{ClientID: 0, Payload: "1 500", Term: 0},
		{ClientID: 0, Payload: "2 500", Term: 0},
		{ClientID: 0, Payload: "3 500", Term: 0},
	}
	b1 := blockchain.BuildBlock(nil, seed, 0)

	// Transfer: client 1 → client 2, amount 200.
	transfer := []structs.Transaction{
		{ClientID: 1, Payload: "2 200", Term: 1},
		{ClientID: 0, Payload: "3 0", Term: 1},  // padding
		{ClientID: 0, Payload: "3 0", Term: 1},  // padding
	}
	b2 := blockchain.BuildBlock([]structs.Block{b1}, transfer, 1)
	chain := []structs.Block{b1, b2}

	if bal := blockchain.GetCommittedBalance(chain, 1); bal != 300 {
		t.Errorf("client 1: want 300, got %d", bal)
	}
	if bal := blockchain.GetCommittedBalance(chain, 2); bal != 700 {
		t.Errorf("client 2: want 700, got %d", bal)
	}
}

// TC_0074 — GetPendingBalance adds uncommitted entries on top of committed state.
func TestGetPendingBalance_IncludesUncommitted(t *testing.T) {
	// Committed chain: client 1 has 500.
	seed := []structs.Transaction{
		{ClientID: 0, Payload: "1 500", Term: 0},
		{ClientID: 0, Payload: "2 500", Term: 0},
		{ClientID: 0, Payload: "3 500", Term: 0},
	}
	block := blockchain.BuildBlock(nil, seed, 0)
	chain := []structs.Block{block}

	// Uncommitted: client 1 sends 100 to client 2.
	uncommitted := []structs.Transaction{
		{ClientID: 1, Payload: "2 100", Term: 1},
	}

	pending1 := blockchain.GetPendingBalance(chain, uncommitted, 1)
	if pending1 != 400 {
		t.Errorf("client 1 pending: want 400, got %d", pending1)
	}

	pending2 := blockchain.GetPendingBalance(chain, uncommitted, 2)
	if pending2 != 600 {
		t.Errorf("client 2 pending: want 600, got %d", pending2)
	}
}

// TC_0075 — GetPendingBalance with no uncommitted entries equals committed balance.
func TestGetPendingBalance_EqualsCommittedWhenNoUncommitted(t *testing.T) {
	seed := []structs.Transaction{
		{ClientID: 0, Payload: "1 750", Term: 0},
		{ClientID: 0, Payload: "2 750", Term: 0},
		{ClientID: 0, Payload: "3 750", Term: 0},
	}
	block := blockchain.BuildBlock(nil, seed, 0)
	chain := []structs.Block{block}

	committed := blockchain.GetCommittedBalance(chain, 1)
	pending := blockchain.GetPendingBalance(chain, nil, 1)

	if pending != committed {
		t.Errorf("pending (%d) != committed (%d) when no uncommitted entries", pending, committed)
	}
}

// ── LoadFirstBlockchain ───────────────────────────────────────────────────────

// TC_0076 — LoadFirstBlockchain returns an error for a missing file.
func TestLoadFirstBlockchain_MissingFile(t *testing.T) {
	_, err := blockchain.LoadFirstBlockchain("/nonexistent/path.txt")
	if err == nil {
		t.Error("expected error for missing file, got nil")
	}
}

// TC_0077 — LoadFirstBlockchain parses the standard seed file and mines valid blocks.
func TestLoadFirstBlockchain_ParsesSeedFile(t *testing.T) {
	path := filepath.Join(testdataDir(t), "first_blockchain.txt")
	chain, err := blockchain.LoadFirstBlockchain(path)
	if err != nil {
		t.Fatalf("LoadFirstBlockchain: %v", err)
	}
	if len(chain) == 0 {
		t.Fatal("expected at least one block from seed file")
	}
	// Every block must satisfy the PoW invariant.
	for i, b := range chain {
		last := b.Hash[len(b.Hash)-1]
		if last != '0' && last != '1' && last != '2' {
			t.Errorf("block %d: PoW invariant violated, hash=%s", i, b.Hash)
		}
	}
}

// TC_0078 — LoadFirstBlockchain gives genesis balances of 1000 per client.
func TestLoadFirstBlockchain_SeedsBalances(t *testing.T) {
	path := filepath.Join(testdataDir(t), "first_blockchain.txt")
	chain, err := blockchain.LoadFirstBlockchain(path)
	if err != nil {
		t.Fatalf("LoadFirstBlockchain: %v", err)
	}

	for _, clientID := range []int{1, 2, 3} {
		bal := blockchain.GetCommittedBalance(chain, clientID)
		if bal != 1000 {
			t.Errorf("client %d: want balance 1000 after seed, got %d", clientID, bal)
		}
	}
}

// TC_0079 — LoadFirstBlockchain ignores comment lines and blank lines.
func TestLoadFirstBlockchain_IgnoresComments(t *testing.T) {
	path := filepath.Join(testdataDir(t), "blockchain_with_comments.txt")
	chain, err := blockchain.LoadFirstBlockchain(path)
	if err != nil {
		t.Fatalf("LoadFirstBlockchain: %v", err)
	}
	// File has 3 real transactions (one block) plus comment/blank lines.
	if len(chain) != 1 {
		t.Errorf("expected 1 block ignoring comments, got %d", len(chain))
	}
}

// ── PrintChain ────────────────────────────────────────────────────────────────

// TC_0080 — PrintChain returns "[]" for an empty chain.
func TestPrintChain_EmptyChain(t *testing.T) {
	out := blockchain.PrintChain(nil)
	if out != "[]" {
		t.Errorf("want '[]', got %q", out)
	}
}

// TC_0081 — PrintChain includes block count and term for a non-empty chain.
func TestPrintChain_NonEmptyChain(t *testing.T) {
	txns := []structs.Transaction{
		{ClientID: 1, Payload: "2 10", Term: 3},
		{ClientID: 2, Payload: "3 20", Term: 3},
		{ClientID: 3, Payload: "1 5", Term: 3},
	}
	block := blockchain.BuildBlock(nil, txns, 3)
	out := blockchain.PrintChain([]structs.Block{block})

	if out == "[]" {
		t.Error("expected non-empty output for a single-block chain")
	}
	if !strings.Contains(out, "t=3") {
		t.Errorf("expected term info in output, got %q", out)
	}
}