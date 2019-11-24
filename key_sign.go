package go_tss

// KeySignReq request to sign a message
type KeySignReq struct {
	Message string `json:"message"` // base64 encoded message to be signed
}

// KeySignResp key sign response
type KeySignResp struct {
	Status Status `json:"status"`
}
