package structs

// BlockSize is the number of transactions packed into one block.
const BlockSize = 3

// Block is a single entry in the blockchain.
type Block struct {
	Term     int           `json:"term"`
	PrevHash string        `json:"prev_hash"`
	Nonce    string        `json:"nonce"`
	Txns     []Transaction `json:"txns"`
	Hash     string        `json:"hash"`
}