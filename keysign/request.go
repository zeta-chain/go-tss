package keysign

import (
	"bytes"
	"strings"

	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/common"
)

// Request request to sign a message
type Request struct {
	PoolPubKey    string   `json:"pool_pub_key"` // pub key of the pool that we would like to send this message from
	Messages      []string `json:"messages"`     // base64 encoded message to be signed
	SignerPubKeys []string `json:"signer_pub_keys"`
	BlockHeight   int64    `json:"block_height"`
	Version       string   `json:"tss_version"`

	logFields map[string]any
}

func NewRequest(pk string, msgs []string, blockHeight int64, signers []string, version string) Request {
	return Request{
		PoolPubKey:    pk,
		Messages:      msgs,
		SignerPubKeys: signers,
		BlockHeight:   blockHeight,
		Version:       version,
	}
}

// MsgID returns the hash of the request.
func (r *Request) MsgID() (string, error) {
	var b bytes.Buffer

	b.WriteString(r.Version)
	b.WriteByte(';')
	b.WriteString(strings.Join(r.Messages, ","))
	b.WriteByte(';')
	b.WriteString(strings.Join(r.SignerPubKeys, ","))

	return common.MsgToHashString(b.Bytes())
}

func (r *Request) MarshalZerologObject(e *zerolog.Event) {
	e.Strs("request.messages", r.Messages)
	e.Int64("request.block_height", r.BlockHeight)

	if len(r.logFields) != 0 {
		e.Fields(r.logFields)
	}
}

func (r *Request) SetLogFields(kv map[string]any) {
	r.logFields = kv
}
