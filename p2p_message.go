package go_tss

import (
	"github.com/binance-chain/tss-lib/tss"
	"github.com/libp2p/go-libp2p-core/peer"
)

// THORChainTSSMessageType  represent the messgae type used in THORChain TSS
type THORChainTSSMessageType uint8

const (
	TSSMsg THORChainTSSMessageType = iota
	VerMsg
)

type VerifyMsg struct {
	MsgType int
	Msg     []byte
	From    string
}

type ThorMsg struct {
	MsgType int
	Msg     []byte
}

type MsgHash struct {
	HashData []byte
}

// Message that get transfer across the wire
type Message struct {
	PeerID  peer.ID
	Payload []byte
}

// WireMessage the message that will be used to transfer across wire
type WireMessage struct {
	MsgType   THORChainTSSMessageType `json:"msg_type"`
	Routing   *tss.MessageRouting     `json:"routing"`
	RoundInfo string                  `json:"round_info"`
	Message   []byte                  `json:"message"`
	Hash      string                  `json:"hash"`
}
