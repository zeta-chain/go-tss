package keygen

// Request request to do keygen
type Request struct {
	Keys []string `json:"keys"`
}

// NewRequest creeate a new instance of keygen.Request
func NewRequest(keys []string) Request {
	return Request{
		Keys: keys,
	}
}
