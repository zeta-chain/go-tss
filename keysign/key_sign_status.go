package keysign

import (
	"gitlab.com/thorchain/tss/go-tss/common"
)

// KeySignReq request to sign a message
type KeySignReq struct {
	PoolPubKey string `json:"pool_pub_key"` // pub key of the pool that we would like to send this message from
	Message    string `json:"message"`      // base64 encoded message to be signed
}

// KeySignResp key sign response
type KeySignResp struct {
	R      string        `json:"r"`
	S      string        `json:"s"`
	Status common.Status `json:"status"`
	Blame  common.Blame  `json:"Blame"`
}
