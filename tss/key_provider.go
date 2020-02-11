package tss

import (
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/peer"
	tcrypto "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

// GetPeerIDFromPubKey get the peer.ID from bech32 format node pub key
func GetPeerIDFromPubKey(pubkey string) (peer.ID, error) {
	pk, err := sdk.GetAccPubKeyBech32(pubkey)
	if err != nil {
		return "", fmt.Errorf("fail to parse account pub key(%s): %w", pubkey, err)
	}
	secpPubKey := pk.(secp256k1.PubKeySecp256k1)
	ppk, err := crypto.UnmarshalSecp256k1PublicKey(secpPubKey[:])
	if err != nil {
		return "", fmt.Errorf("fail to convert pubkey to the crypto pubkey used in libp2p: %w", err)
	}
	return peer.IDFromPublicKey(ppk)
}

// GetPeerIDsFromPubKeys convert a list of node pub key to their peer.ID
func GetPeerIDsFromPubKeys(pubkeys []string) ([]string, error) {
	var peerIDs []string
	for _, item := range pubkeys {
		peerID, err := GetPeerIDFromPubKey(item)
		if err != nil {
			return nil, err
		}
		peerIDs = append(peerIDs, peerID.String())
	}
	return peerIDs, nil
}

// GetPubKeysFromPeerIDs given a list of peer ids, and get a list og pub keys.
func GetPubKeysFromPeerIDs(peers []string) ([]string, error) {
	var result []string
	for _, item := range peers {
		peerID, err := peer.IDB58Decode(item)
		if err != nil {
			return nil, fmt.Errorf("fail to decode peer id: %w", err)
		}
		pk, err := peerID.ExtractPublicKey()
		if err != nil {
			return nil, fmt.Errorf("fail to extract pub key from peer id: %w", err)
		}
		rawBytes, err := pk.Raw()
		if err != nil {
			return nil, fmt.Errorf("faail to get pub key raw bytes: %w", err)
		}
		var pubkey secp256k1.PubKeySecp256k1
		copy(pubkey[:], rawBytes)
		accPubKey, err := sdk.Bech32ifyAccPub(pubkey)
		if err != nil {
			return nil, fmt.Errorf("fail to bechfy account pub key: %w", err)
		}
		result = append(result, accPubKey)
	}
	return result, nil
}

func getPriKey(priKeyString string) (tcrypto.PrivKey, error) {
	priHexBytes, err := base64.StdEncoding.DecodeString(priKeyString)
	if err != nil {
		return nil, fmt.Errorf("fail to decode private key: %w", err)
	}
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if err != nil {
		return nil, fmt.Errorf("fail to hex decode private key: %w", err)
	}
	var keyBytesArray [32]byte
	copy(keyBytesArray[:], rawBytes[:32])
	priKey := secp256k1.PrivKeySecp256k1(keyBytesArray)
	return priKey, nil
}

func getPriKeyRawBytes(priKey tcrypto.PrivKey) ([]byte, error) {
	var keyBytesArray [32]byte
	pk, ok := priKey.(secp256k1.PrivKeySecp256k1)
	if !ok {
		return nil, errors.New("private key is not secp256p1.PrivKeySecp256k1")
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}
