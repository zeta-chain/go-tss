package tss

// KeyGenRequest
type KeyGenReq struct {
	Keys []string `json:"keys"`
}

// KeyGenResponse
type KeyGenResp struct {
	PubKey     string `json:"pub_key"`
	BNBAddress string `json:"bnb_address"`
	Status     Status `json:"status"`
}
