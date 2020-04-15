package keysign

import (
	"gitlab.com/thorchain/tss/go-tss/blame"
	"gitlab.com/thorchain/tss/go-tss/common"
)

// Response key sign response
type Response struct {
	R      string        `json:"r"`
	S      string        `json:"s"`
	Status common.Status `json:"status"`
	Blame  blame.Blame   `json:"blame"`
}

func NewResponse(r, s string, status common.Status, blame blame.Blame) Response {
	return Response{
		R:      r,
		S:      s,
		Status: status,
		Blame:  blame,
	}
}
