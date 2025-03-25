package conversion

import (
	"fmt"
	"math/rand"

	"github.com/blang/semver"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	atypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
)

// GetRandomPubKey for test
func GetRandomPubKey() string {
	_, pubKey, _ := atypes.KeyTestPubAddr()
	bech32PubKey, _ := sdk.MarshalPubKey(sdk.AccPK, pubKey)
	return bech32PubKey
}

// GetRandomPeerID for test
func GetRandomPeerID() peer.ID {
	_, pubKey, _ := atypes.KeyTestPubAddr()
	var pk secp256k1.PubKey
	copy(pk[:], pubKey.Bytes())
	peerID, _ := GetPeerIDFromSecp256PubKey(pk)
	return peerID
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
)

func RandStringBytesMask(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; {
		if idx := int(rand.Int63() & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i++
		}
	}
	return string(b)
}

func VersionLTCheck(currentVer, expectedVer string) (bool, error) {
	c, err := semver.Make(expectedVer)
	if err != nil {
		return false, errors.New("fail to parse the expected version")
	}
	v, err := semver.Make(currentVer)
	if err != nil {
		return false, errors.New("fail to parse the current version")
	}
	return v.LT(c), nil
}

// TestBootstrapPeers generates a list of bootstrap addresses for local tests
func TestBootstrapAddrs(ports []int, testPubKeys []string) ([]maddr.Multiaddr, error) {
	res := make([]maddr.Multiaddr, len(ports))
	for i := 0; i < len(ports); i++ {
		peerID, err := Bech32PubkeyToPeerID(testPubKeys[i])
		if err != nil {
			return nil, errors.Wrapf(err, "invalid bech32 pubkey for peer %q", testPubKeys[i])
		}

		peerAddr, err := maddr.NewMultiaddr(fmt.Sprintf("/ip4/127.0.0.1/tcp/%d/p2p/%s", ports[i], peerID.String()))
		if err != nil {
			return nil, errors.Wrapf(err, "invalid peer addr %d", i)
		}

		res[i] = peerAddr
	}

	return res, nil
}
