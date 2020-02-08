package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

// JoinParty represent a join party request
type JoinParty struct {
	Msg  *messages.JoinPartyRequest
	Peer peer.ID
	Resp chan *messages.JoinPartyResponse
}

// NewJoinParty create a new instance of JoinParty
func NewJoinParty(msg *messages.JoinPartyRequest, peer peer.ID) *JoinParty {
	return &JoinParty{
		Msg:  msg,
		Peer: peer,
		Resp: make(chan *messages.JoinPartyResponse, 1),
	}
}
