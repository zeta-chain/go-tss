package go_tss

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	crypto2 "github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

var emptyPeerID peer.ID

func getPeerIDFromPubKey(bech32PubKey string) (peer.ID, error) {
	pk, err := sdk.GetAccPubKeyBech32(bech32PubKey)
	if nil != err {
		return emptyPeerID, fmt.Errorf("fail to get pub key from bech32 string: %w", err)
	}
	pkbytes, ok := pk.(secp256k1.PubKeySecp256k1)
	if !ok {
		return emptyPeerID, fmt.Errorf("%s is not secp256k1 pubkey", bech32PubKey)
	}
	ppk, err := crypto2.UnmarshalSecp256k1PublicKey(pkbytes[:])
	if nil != err {
		return emptyPeerID, fmt.Errorf("fail to convert pubkey to the crypto pubkey used in libp2p: %w", err)
	}
	return peer.IDFromPublicKey(ppk)
}

func getPeerIDFromSecp256PubKey(pk secp256k1.PubKeySecp256k1) (peer.ID, error) {
	ppk, err := crypto2.UnmarshalSecp256k1PublicKey(pk[:])
	if nil != err {
		return emptyPeerID, fmt.Errorf("fail to convert pubkey to the crypto pubkey used in libp2p: %w", err)
	}
	return peer.IDFromPublicKey(ppk)
}
