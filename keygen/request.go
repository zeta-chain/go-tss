package keygen

import (
	"bytes"
	"sort"
	"strings"

	"github.com/zeta-chain/go-tss/common"
)

// Request request to do keygen
type Request struct {
	Keys        []string    `json:"keys"`
	BlockHeight int64       `json:"block_height"`
	Version     string      `json:"tss_version"`
	Algo        common.Algo `json:"algo,omitempty"`
}

// NewRequest constructs Request.
func NewRequest(keys []string, blockHeight int64, version string, algo common.Algo) Request {
	sort.Strings(keys)

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
