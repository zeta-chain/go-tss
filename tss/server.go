package tss

import (
	"github.com/libp2p/go-libp2p/core/peer"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

// Server define the necessary functionality should be provide by a TSS Server implementation
type Server interface {
	Start() error
	Stop()
	GetLocalPeerID() string
	GetKnownPeers() []peer.AddrInfo
	Keygen(req keygen.Request) (keygen.Response, error)
	KeygenAllAlgo(req keygen.Request) ([]keygen.Response, error)
	KeySign(req keysign.Request) (keysign.Response, error)
}
