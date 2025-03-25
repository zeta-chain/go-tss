package conversion

import (
	"encoding/base64"
	"encoding/hex"

	"github.com/btcsuite/btcd/btcec/v2"
	tcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
)

// GetPeerIDFromPubKey get the peer.ID from bech32 format node pub key
func GetPeerIDFromPubKey(pubkey string) (peer.ID, error) {
	pk, err := sdk.UnmarshalPubKey(sdk.AccPK, pubkey)
	if err != nil {
		return "", errors.Wrapf(err, "failed to parse pubkey %q", pubkey)
	}

	ppk, err := crypto.UnmarshalSecp256k1PublicKey(pk.Bytes())
	if err != nil {
		return "", errors.Wrapf(err, "failed to convert unmarshal secp256k1 %q", pubkey)
	}

	return peer.IDFromPublicKey(ppk)
}

// GetPeerIDsFromPubKeys convert a list of node pub key to their peer.ID
func GetPeerIDsFromPubKeys(pubkeys []string) ([]peer.ID, error) {
	var peerIDs []peer.ID
	for _, item := range pubkeys {
		peerID, err := GetPeerIDFromPubKey(item)
		if err != nil {
			return nil, err
		}
		peerIDs = append(peerIDs, peerID)
	}
	return peerIDs, nil
}

// GetPeerIDs return a slice of peer id
func GetPeerIDs(pubkeys []string) ([]peer.ID, error) {
	var peerIDs []peer.ID
	for _, item := range pubkeys {
		pID, err := GetPeerIDFromPubKey(item)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get peer id from pubkey %q", item)
		}

		peerIDs = append(peerIDs, pID)
	}

	return peerIDs, nil
}

// GetPubKeysFromPeerIDs given a list of peer ids, and get a list og pub keys.
func GetPubKeysFromPeerIDs(peers []string) ([]string, error) {
	var result []string
	for _, item := range peers {
		pKey, err := GetPubKeyFromPeerID(item)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to get pubkey from peerID %q", item)
		}

		result = append(result, pKey)
	}

	return result, nil
}

// GetPubKeyFromPeerID extract the pub key from PeerID
func GetPubKeyFromPeerID(pID string) (string, error) {
	peerID, err := peer.Decode(pID)
	if err != nil {
		return "", errors.Wrapf(err, "failed to decode peer id %q", pID)
	}

	pk, err := peerID.ExtractPublicKey()
	if err != nil {
		return "", errors.Wrapf(err, "failed to extract pub key from peer id %q", pID)
	}

	rawBytes, err := pk.Raw()
	if err != nil {
		return "", errors.Wrapf(err, "failed to get pub key raw bytes %q", pID)
	}

	pubKey := coskey.PubKey{Key: rawBytes}

	return sdk.MarshalPubKey(sdk.AccPK, &pubKey)
}

func GetPriKey(priKeyString string) (tcrypto.PrivKey, error) {
	priHexBytes, err := base64.StdEncoding.DecodeString(priKeyString)
	if err != nil {
		return nil, errors.Wrap(err, "failed to decode private key")
	}

	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if err != nil {
		return nil, errors.Wrap(err, "failed to hex decode private key")
	}

	return secp256k1.PrivKey(rawBytes[:32]), nil
}

func GetPriKeyRawBytes(priKey tcrypto.PrivKey) ([]byte, error) {
	var keyBytesArray [32]byte
	pk, ok := priKey.(secp256k1.PrivKey)
	if !ok {
		return nil, errors.New("private key is not secp256p1.PrivKey")
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}

func CheckKeyOnCurve(pk string) (bool, error) {
	pubKey, err := sdk.UnmarshalPubKey(sdk.AccPK, pk)
	if err != nil {
		return false, errors.Wrapf(err, "failed to parse pubkey %q", pk)
	}

	switch pubKey.Type() {
	case secp256k1.KeyType:
		bPk, err := btcec.ParsePubKey(pubKey.Bytes())
		if err != nil {
			return false, err
		}

		return isOnCurve(bPk.X(), bPk.Y(), btcec.S256()), nil
	case ed25519.KeyType:
		bPk, err := edwards.ParsePubKey(pubKey.Bytes())
		if err != nil {
			return false, err
		}

		return isOnCurve(bPk.X, bPk.Y, edwards.Edwards()), nil
	default:
		return false, errors.Errorf("failed to parse pubkey %q", pk)
	}
}
