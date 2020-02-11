package keysign

// Request request to sign a message
type Request struct {
	PoolPubKey string `json:"pool_pub_key"` // pub key of the pool that we would like to send this message from
	Message    string `json:"message"`      // base64 encoded message to be signed
}

func NewRequst(pk, msg string) Request {
	return Request{
		PoolPubKey: pk,
		Message:    msg,
	}
}
