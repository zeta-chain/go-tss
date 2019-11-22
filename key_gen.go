package go_tss

// KeyGenRequest
type KeyGenReq struct {
	PrivKey string   `json:"priv_key"`
	Keys    []string `json:"keys"`
}

// KeyGenResponse
type KeyGenResp struct {
	PubKey string `json:"pub_key"`
	Status Status `json:"status"`
}
