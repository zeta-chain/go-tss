package go_tss

import (
	"github.com/libp2p/go-libp2p-core/peer"
)

// Message
type Message struct {
	PeerID  peer.ID
	Payload []byte
}
