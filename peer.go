package go_tss

import (
	"fmt"

	crypto2 "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

var emptyPeerID peer.ID

func getPeerIDFromSecp256PubKey(pk secp256k1.PubKeySecp256k1) (peer.ID, error) {
	ppk, err := crypto2.UnmarshalSecp256k1PublicKey(pk[:])
	if nil != err {
		return emptyPeerID, fmt.Errorf("fail to convert pubkey to the crypto pubkey used in libp2p: %w", err)
	}
	return peer.IDFromPublicKey(ppk)
}
