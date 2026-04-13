// Package blockchain handles proof-of-work mining, balance calculation,
// and loading the initial chain from first_blockchain.txt.
//
// Payload format for banking transfers stored inside a Transaction:
//
//	Payload = "<receiverID> <amount>"   (space-separated integers)
//	ClientID = senderID
//
// A special senderID of 0 means "genesis / bank" and only adds to the
// receiver — it never reduces any balance.
package blockchain

import (
	"RTTH/internal/structs"
	"bufio"
	"crypto/sha256"
	"fmt"
	"math/rand"
	"os"
	"strconv"
	"strings"
	"time"
)

// ─── Proof-of-Work ────────────────────────────────────────────────────────────

const charset = "abcdefghijklmnopqrstuvwxyz0123456789"

// blockDataString serialises the inputs that are hashed for PoW.
func blockDataString(prevHash string, txns []structs.Transaction, nonce string) string {
	var sb strings.Builder
	sb.WriteString(prevHash)
	sb.WriteString(nonce)
	for _, t := range txns {
		fmt.Fprintf(&sb, "%d%d%s%d%d", t.ID, t.ClientID, t.Payload, t.Timestamp, t.Term)
	}
	return sb.String()
}

// computeHash returns the SHA-256 hex digest of the given inputs.
func computeHash(prevHash string, txns []structs.Transaction, nonce string) string {
	h := sha256.Sum256([]byte(blockDataString(prevHash, txns, nonce)))
	return fmt.Sprintf("%x", h)
}

// isValidHash returns true when the last hex character of the hash is 0, 1, or 2.
// This gives ~18.75 % success per attempt — fast to mine, easy to verify.
func isValidHash(hash string) bool {
	if len(hash) == 0 {
		return false
	}
	last := hash[len(hash)-1]
	return last == '0' || last == '1' || last == '2'
}

// mine keeps generating random 8-character nonces until a valid hash is found.
func mine(prevHash string, txns []structs.Transaction) (nonce, hash string) {
	rng := rand.New(rand.NewSource(time.Now().UnixNano()))
	buf := make([]byte, 8)
	for {
		for i := range buf {
			buf[i] = charset[rng.Intn(len(charset))]
		}
		n := string(buf)
		h := computeHash(prevHash, txns, n)
		if isValidHash(h) {
			return n, h
		}
	}
}

// prevBlockHash returns the hash of the last block, or "0" for an empty chain.
func prevBlockHash(chain []structs.Block) string {
	if len(chain) == 0 {
		return "0"
	}
	return chain[len(chain)-1].Hash
}

// BuildBlock mines a new block for the given transactions and appends it
// to a copy of the chain, returning the newly mined block.
func BuildBlock(chain []structs.Block, txns []structs.Transaction, term int) structs.Block {
	ph := prevBlockHash(chain)
	nonce, hash := mine(ph, txns)
	return structs.Block{
		Term:     term,
		PrevHash: ph,
		Nonce:    nonce,
		Txns:     append([]structs.Transaction(nil), txns...),
		Hash:     hash,
	}
}

// ─── Initial chain loader ─────────────────────────────────────────────────────

// LoadFirstBlockchain parses first_blockchain.txt and returns a fully mined
// blockchain.  File format: one transfer per line ("sender receiver amount"),
// every 3 lines forming one block.  Lines beginning with '#' are ignored.
func LoadFirstBlockchain(path string) ([]structs.Block, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("blockchain: open %s: %w", path, err)
	}
	defer f.Close()

	var txns []structs.Transaction
	scanner := bufio.NewScanner(f)
	id := 1
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		parts := strings.Fields(line)
		if len(parts) < 3 {
			continue
		}
		sender, err1 := strconv.Atoi(parts[0])
		receiver, err2 := strconv.Atoi(parts[1])
		amount, err3 := strconv.Atoi(parts[2])
		if err1 != nil || err2 != nil || err3 != nil {
			continue
		}
		txns = append(txns, structs.Transaction{
			ID:       id,
			ClientID: sender,
			Payload:  fmt.Sprintf("%d %d", receiver, amount),
			Term:     0,
		})
		id++
	}
	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("blockchain: scan %s: %w", path, err)
	}

	var chain []structs.Block
	for i := 0; i+structs.BlockSize <= len(txns); i += structs.BlockSize {
		batch := txns[i : i+structs.BlockSize]
		block := BuildBlock(chain, batch, 0)
		chain = append(chain, block)
	}
	return chain, nil
}

// ─── Balance calculation ──────────────────────────────────────────────────────

// parseTransfer extracts (sender, receiver, amount) from a Transaction.
// The sender is txn.ClientID; the payload is "receiverID amount".
// Returns ok=false for non-transfer payloads.
func parseTransfer(txn structs.Transaction) (sender, receiver, amount int, ok bool) {
	parts := strings.Fields(txn.Payload)
	if len(parts) < 2 {
		return
	}
	r, err1 := strconv.Atoi(parts[0])
	a, err2 := strconv.Atoi(parts[1])
	if err1 != nil || err2 != nil || a <= 0 {
		return
	}
	return txn.ClientID, r, a, true
}

// applyTxn updates balance for clientID given a single transaction.
func applyTxn(balance *int, txn structs.Transaction, clientID int) {
	sender, receiver, amount, ok := parseTransfer(txn)
	if !ok {
		return
	}
	if receiver == clientID {
		*balance += amount
	}
	// sender == 0 means genesis credit; never debit the virtual bank.
	if sender == clientID && sender != 0 {
		*balance -= amount
	}
}

// GetCommittedBalance returns the net balance of clientID based solely on
// the committed blockchain.
func GetCommittedBalance(chain []structs.Block, clientID int) int {
	bal := 0
	for _, block := range chain {
		for _, txn := range block.Txns {
			applyTxn(&bal, txn, clientID)
		}
	}
	return bal
}

// GetPendingBalance returns the balance including uncommitted log entries.
func GetPendingBalance(chain []structs.Block, uncommitted []structs.Transaction, clientID int) int {
	bal := GetCommittedBalance(chain, clientID)
	for _, txn := range uncommitted {
		applyTxn(&bal, txn, clientID)
	}
	return bal
}

// ─── Logging helper ───────────────────────────────────────────────────────────

// PrintChain returns a compact one-line summary of the blockchain for logging.
func PrintChain(chain []structs.Block) string {
	if len(chain) == 0 {
		return "[]"
	}
	var sb strings.Builder
	sb.WriteByte('[')
	for i, b := range chain {
		if i > 0 {
			sb.WriteString(", ")
		}
		suffix := b.Hash
		if len(suffix) > 6 {
			suffix = "…" + suffix[len(suffix)-6:]
		}
		fmt.Fprintf(&sb, "b%d(t=%d,%s)", i+1, b.Term, suffix)
	}
	sb.WriteByte(']')
	return sb.String()
}