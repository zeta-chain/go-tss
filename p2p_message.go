package go_tss

import (
	"github.com/libp2p/go-libp2p-core/peer"
)

const (
	TssMsg=iota
	VerMsg
)

type VerifyMsg struct {
	MsgType int
	Msg []byte
	From string
}

type ThorMsg struct{
	MsgType int
	Msg []byte
}

type MsgHash struct{
	HashData []byte
}

// Message
type Message struct {
	PeerID  peer.ID
	Payload []byte
}
