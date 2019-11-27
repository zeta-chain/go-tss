package go_tss

// KeySignReq request to sign a message
type KeySignReq struct {
	PoolPubKey string `json:"pool_pub_key"` // pub key of the pool that we would like to send this message from
	Message    string `json:"message"`      // base64 encoded message to be signed
}

// KeySignResp key sign response
type KeySignResp struct {
	Signature string
	Status    Status `json:"status"`
}
