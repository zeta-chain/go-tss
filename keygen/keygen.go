package keygen

import (
	"bytes"
	"strings"

	"github.com/bnb-chain/tss-lib/crypto"

	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/p2p"
)

// Service TSS key generation service
type Service interface {
	GenerateNewKey(request Request) (*crypto.ECPoint, error)
	KeygenChannel() chan *p2p.Message
	Common() *common.TssCommon
}

// Request request to do keygen
type Request struct {
	Keys        []string    `json:"keys"`
	BlockHeight int64       `json:"block_height"`
	Version     string      `json:"tss_version"`
	Algo        common.Algo `json:"algo,omitempty"`
}

// NewRequest constructs Request.
func NewRequest(keys []string, blockHeight int64, version string, algo common.Algo) Request {
	return Request{
		Keys:        keys,
		BlockHeight: blockHeight,
		Version:     version,
		Algo:        algo,
	}
}

// MsgID returns the hash of the request.
func (r *Request) MsgID() (string, error) {
	var b bytes.Buffer

	b.WriteString(r.Version)
	b.WriteByte(';')
	b.WriteString(string(r.Algo))
	b.WriteByte(';')
	b.WriteString(strings.Join(r.Keys, ","))

	return common.MsgToHashString(b.Bytes())
}

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
