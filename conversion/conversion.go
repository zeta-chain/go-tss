package conversion

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"

	"github.com/bnb-chain/tss-lib/v2/crypto"
	btss "github.com/bnb-chain/tss-lib/v2/tss"
	"github.com/btcsuite/btcd/btcec/v2"
	coseddkey "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/types"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	crypto2 "github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"

	"github.com/decred/dcrd/dcrec/edwards/v2"
	"gitlab.com/thorchain/tss/go-tss/messages"
)

// GetPeerIDFromSecp256PubKey convert the given pubkey into a peer.ID
func GetPeerIDFromSecp256PubKey(pk []byte) (peer.ID, error) {
	ppk, err := crypto2.UnmarshalSecp256k1PublicKey(pk)
	if err != nil {
		return "", fmt.Errorf("fail to convert pubkey to the crypto pubkey used in libp2p: %w", err)
	}
	return peer.IDFromPublicKey(ppk)
}

func GetPeerIDFromPartyID(partyID *btss.PartyID) (peer.ID, error) {
	if partyID == nil || !partyID.ValidateBasic() {
		return "", errors.New("invalid partyID")
	}
	pkBytes := partyID.KeyInt().Bytes()
	return GetPeerIDFromSecp256PubKey(pkBytes)
}

func PartyIDtoPubKey(party *btss.PartyID) (string, error) {
	if party == nil || !party.ValidateBasic() {
		return "", errors.New("invalid party")
	}
	partyKeyBytes := party.GetKey()
	pk := coskey.PubKey{
		Key: partyKeyBytes,
	}
	pubKey, err := sdk.MarshalPubKey(sdk.AccPK, &pk)
	if err != nil {
		return "", err
	}
	return pubKey, nil
}

func AccPubKeysFromPartyIDs(partyIDs []string, partyIDMap map[string]*btss.PartyID) ([]string, error) {
	pubKeys := make([]string, 0)
	for _, partyID := range partyIDs {
		blameParty, ok := partyIDMap[partyID]
		if !ok {
			return nil, errors.New("cannot find the blame party")
		}
		blamedPubKey, err := PartyIDtoPubKey(blameParty)
		if err != nil {
			return nil, err
		}
		pubKeys = append(pubKeys, blamedPubKey)
	}
	return pubKeys, nil
}

func SetupPartyIDMap(partiesID []*btss.PartyID) map[string]*btss.PartyID {
	partyIDMap := make(map[string]*btss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}
	return partyIDMap
}

func GetPeersID(partyIDtoP2PID map[string]peer.ID, localPeerID string) []peer.ID {
	if partyIDtoP2PID == nil {
		return nil
	}
	peerIDs := make([]peer.ID, 0, len(partyIDtoP2PID)-1)
	for _, value := range partyIDtoP2PID {
		if value.String() == localPeerID {
			continue
		}
		peerIDs = append(peerIDs, value)
	}
	return peerIDs
}

func SetupIDMaps(parties map[string]*btss.PartyID, partyIDtoP2PID map[string]peer.ID) error {
	for id, party := range parties {
		peerID, err := GetPeerIDFromPartyID(party)
		if err != nil {
			return err
		}
		partyIDtoP2PID[id] = peerID
	}
	return nil
}

func GetParties(keys []string, localPartyKey string) (btss.SortedPartyIDs, *btss.PartyID, error) {
	var localPartyID *btss.PartyID
	var unSortedPartiesID []*btss.PartyID
	sort.Strings(keys)
	for idx, item := range keys {
		pk, err := sdk.UnmarshalPubKey(sdk.AccPK, item)
		if err != nil {
			return nil, nil, fmt.Errorf("fail to get account pub key address(%s): %w", item, err)
		}
		key := new(big.Int).SetBytes(pk.Bytes())
		// Set up the parameters
		// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
		// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
		// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
		partyID := btss.NewPartyID(strconv.Itoa(idx), "", key)
		if item == localPartyKey {
			localPartyID = partyID
		}
		unSortedPartiesID = append(unSortedPartiesID, partyID)
	}
	if localPartyID == nil {
		return nil, nil, errors.New("local party is not in the list")
	}

	partiesID := btss.SortPartyIDs(unSortedPartiesID)
	return partiesID, localPartyID, nil
}

func GetPreviousKeySignUicast(current string) string {
	if strings.HasSuffix(current, messages.KEYSIGN1b) {
		return messages.KEYSIGN1aUnicast
	}
	return messages.KEYSIGN2Unicast
}

func isOnCurve(x, y *big.Int, curve elliptic.Curve) bool {
	return curve.IsOnCurve(x, y)
}

func GetTssPubKeyECDSA(pubKeyPoint *crypto.ECPoint) (string, types.AccAddress, error) {
	// we check whether the point is on curve according to Kudelski report
	if pubKeyPoint == nil || !isOnCurve(pubKeyPoint.X(), pubKeyPoint.Y(), btcec.S256()) {
		return "", types.AccAddress{}, errors.New("[ECDSA] invalid points")
	}
	X := &btcec.FieldVal{}
	X.SetByteSlice(pubKeyPoint.X().Bytes())
	Y := &btcec.FieldVal{}
	Y.SetByteSlice(pubKeyPoint.Y().Bytes())
	tssPubKey := btcec.NewPublicKey(X, Y)

	compressedPubkey := coskey.PubKey{
		Key: tssPubKey.SerializeCompressed(),
	}

	pubKey, err := sdk.MarshalPubKey(sdk.AccPK, &compressedPubkey)
	addr := types.AccAddress(compressedPubkey.Address().Bytes())
	return pubKey, addr, err
}

func GetTssPubKeyEDDSA(pubKeyPoint *crypto.ECPoint) (string, types.AccAddress, error) {
	// we check whether the point is on curve according to Kudelski report
	if pubKeyPoint == nil || !isOnCurve(pubKeyPoint.X(), pubKeyPoint.Y(), btss.Edwards()) {
		return "", nil, errors.New("[EDDSA] invalid points")
	}
	tssPubKey := edwards.PublicKey{
		Curve: edwards.Edwards(),
		X:     pubKeyPoint.X(),
		Y:     pubKeyPoint.Y(),
	}

	compressedPubkey := coseddkey.PubKey{
		Key: tssPubKey.SerializeCompressed(),
	}

	pubKey, err := sdk.MarshalPubKey(sdk.AccPK, &compressedPubkey)
	addr := types.AccAddress(compressedPubkey.Address().Bytes())
	return pubKey, addr, err
}

func BytesToHashString(msg []byte) (string, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if err != nil {
		return "", fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func GetThreshold(value int) (int, error) {
	if value < 0 {
		return 0, errors.New("negative input")
	}
	threshold := int(math.Ceil(float64(value)*2.0/3.0)) - 1
	return threshold, nil
}

func GetEDDSAPrivateKeyRawBytes(privateKey crypto2.PrivKey) ([]byte, error) {
	var keyBytesArray [64]byte
	pk, err := privateKey.Raw()
	if err != nil {
		return nil, err
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}
