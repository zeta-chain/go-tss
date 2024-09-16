package keygen

import "gitlab.com/thorchain/tss/go-tss/common"

// Request request to do keygen
type Request struct {
	Keys        []string    `json:"keys"`
	BlockHeight int64       `json:"block_height"`
	Version     string      `json:"tss_version"`
	Algo        common.Algo `json:"algo,omitempty"`
}

// NewRequest creeate a new instance of keygen.Request
func NewRequest(keys []string, blockHeight int64, version string, algo common.Algo) Request {
	return Request{
		Keys:        keys,
		BlockHeight: blockHeight,
		Version:     version,
		Algo:        algo,
	}
}
