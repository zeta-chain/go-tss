package keygen

import (
	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
)

// Response keygen response
type Response struct {
	Algo        common.Algo   `json:"algo"`
	PubKey      string        `json:"pub_key"`
	PoolAddress string        `json:"pool_address"`
	Status      common.Status `json:"status"`
	Blame       blame.Blame   `json:"blame"`
}

// NewResponse create a new instance of keygen.Response
func NewResponse(algo common.Algo, pk, addr string, status common.Status, blame blame.Blame) Response {
	return Response{
		Algo:        algo,
		PubKey:      pk,
		PoolAddress: addr,
		Status:      status,
		Blame:       blame,
	}
}
