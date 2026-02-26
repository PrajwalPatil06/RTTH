package domain
import "RTTH/internal/store"
type Node struct {
	Id   int
	State     string
	Store *store.MemoryStore
}
func NewNode(id int)*Node{
	return &Node{
		Id:id,
		State: "Follower",
		Store: store.NewMemoryStore(),
	}
}