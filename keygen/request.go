package keygen

// Request request to do keygen
type Request struct {
	Keys        []string `json:"keys"`
	BlockHeight string   `json:"block_height"`
}

// NewRequest creeate a new instance of keygen.Request
func NewRequest(keys []string, blockHeight string) Request {
	return Request{
		Keys:        keys,
		BlockHeight: blockHeight,
	}
}
