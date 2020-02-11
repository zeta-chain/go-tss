package keygen

import (
	"gitlab.com/thorchain/tss/go-tss/common"
)

// KeyGenReq request to do keygen
type KeyGenReq struct {
	Keys []string `json:"keys"`
}

// KeyGenResp keygen response
type KeyGenResp struct {
	PubKey      string        `json:"pub_key"`
	PoolAddress string        `json:"pool_address"`
	Status      common.Status `json:"status"`
	Blame       common.Blame  `json:"blame"`
}

func NewKeyGenReq(keys []string) KeyGenReq {
	return KeyGenReq{
		Keys: keys,
	}
}

func NewKeyGenResp(pk, addr string, status common.Status, blame common.Blame) KeyGenResp {
	return KeyGenResp{
		PubKey:      pk,
		PoolAddress: addr,
		Status:      status,
		Blame:       blame,
	}
}
