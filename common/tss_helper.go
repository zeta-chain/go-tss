package common

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"

	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/btcsuite/btcd/btcec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

func Contains(s []*btss.PartyID, e *btss.PartyID) bool {
	if e == nil {
		return false
	}
	for _, a := range s {
		if *a == *e {
			return true
		}
	}
	return false
}

func GetThreshold(value int) (int, error) {
	if value < 0 {
		return 0, errors.New("negative input")
	}
	threshold := int(math.Ceil(float64(value)*2.0/3.0)) - 1
	return threshold, nil
}

func SetupPartyIDMap(partiesID []*btss.PartyID) map[string]*btss.PartyID {
	partyIDMap := make(map[string]*btss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}
	return partyIDMap
}

func GetPeersID(partyIDtoP2PID map[string]peer.ID, localPeerID string) []peer.ID {
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
		peerID, err := getPeerIDFromPartyID(party)
		if err != nil {
			return err
		}
		partyIDtoP2PID[id] = peerID
	}
	return nil
}

func MsgToHashInt(msg []byte) (*big.Int, error) {
	return hashToInt(msg, btcec.S256()), nil
}

func MsgToHashString(msg []byte) (string, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if err != nil {
		return "", fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func hashToInt(hash []byte, c elliptic.Curve) *big.Int {
	orderBits := c.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}

func InitLog(level string, pretty bool, serviceValue string) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("%s is not a valid log-level, falling back to 'info'", level)
	}
	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	zerolog.SetGlobalLevel(l)
	log.Logger = log.Output(out).With().Str("service", serviceValue).Logger()
}

func AccPubKeysFromPartyIDs(partyIDs []string, partyIDMap map[string]*btss.PartyID) ([]string, error) {
	pubKeys := make([]string, 0)
	for _, partyID := range partyIDs {
		blameParty, ok := partyIDMap[partyID]
		if !ok {
			return nil, errors.New("cannot find the blame party")
		}
		blamePartyKeyBytes := blameParty.GetKey()
		var pk secp256k1.PubKeySecp256k1
		copy(pk[:], blamePartyKeyBytes)
		blamedPubKey, err := sdk.Bech32ifyAccPub(pk)
		if err != nil {
			return nil, err
		}
		pubKeys = append(pubKeys, blamedPubKey)
	}
	return pubKeys, nil
}

// GetBlamePubKeysInList returns the nodes public key who are in the peer list
func (t *TssCommon) getBlamePubKeysInList(peers []string) ([]string, error) {
	var partiesInList []string
	// we convert nodes (in the peers list) P2PID to public key
	for partyID, p2pID := range t.PartyIDtoP2PID {
		for _, el := range peers {
			if el == p2pID.String() {
				partiesInList = append(partiesInList, partyID)
			}
		}
	}

	localPartyInfo := t.getPartyInfo()
	partyIDMap := localPartyInfo.PartyIDMap
	blamePubKeys, err := AccPubKeysFromPartyIDs(partiesInList, partyIDMap)
	if err != nil {
		return nil, err
	}

	return blamePubKeys, nil
}

func (t *TssCommon) getBlamePubKeysNotInList(peers []string) ([]string, error) {
	var partiesNotInList []string
	// we convert nodes (NOT in the peers list) P2PID to public key
	for partyID, p2pID := range t.PartyIDtoP2PID {
		if t.partyInfo.Party.PartyID().Id == partyID {
			continue
		}
		found := false
		for _, each := range peers {
			if p2pID.String() == each {
				found = true
				break
			}
		}
		if found == false {
			partiesNotInList = append(partiesNotInList, partyID)
		}
	}

	localPartyInfo := t.getPartyInfo()
	partyIDMap := localPartyInfo.PartyIDMap
	blamePubKeys, err := AccPubKeysFromPartyIDs(partiesNotInList, partyIDMap)
	if err != nil {
		return nil, err
	}

	return blamePubKeys, nil
}

// GetBlamePubKeysNotInList returns the nodes public key who are not in the peer list
func (t *TssCommon) GetBlamePubKeysLists(peer []string) ([]string, []string, error) {
	inList, err := t.getBlamePubKeysInList(peer)
	if err != nil {
		return nil, nil, err
	}

	notInlist, err := t.getBlamePubKeysNotInList(peer)
	if err != nil {
		return nil, nil, err
	}

	return inList, notInlist, err
}

// TssTimeoutBlame handles the blame caused by the nodesync error
// We believe the node itself will not cheat himself, so we go through
// the confirmed list to find out the absent node(s) that fail to send the
// hash of the message. The node who receive the broadcast message must send
// the VerMsg otherwise, we blame them
func (t *TssCommon) TssTimeoutBlame(localCachedItems []*LocalCacheItem, lastMessageType string) ([]string, error) {
	var sumStandbyPeers []string
	for _, el := range localCachedItems {
		if el.Msg == nil {
			continue
		}
		// we only process the last message, cause we stopped by the last message, if we still process
		// the previous localCachedItem, it must be send from the malicious node.
		if el.Msg.RoundInfo == lastMessageType {
			if len(t.P2PPeers) == el.TotalConfirmParty() {
				return nil, nil
			}
			sumStandbyPeers = append(sumStandbyPeers, el.GetPeers()...)
		}
	}
	_, blamePubKeys, err := t.GetBlamePubKeysLists(sumStandbyPeers)
	if err != nil {
		t.logger.Error().Err(err).Msg("error in get blame parties pubkey")
		return nil, err
	}

	return blamePubKeys, nil
}

func (t *TssCommon) findBlamePeers(localCacheItem *LocalCacheItem, dataOwnerP2PID string) ([]string, error) {
	// our tss is based on the assumption that 2/3 of the nodes are honest. we define the majority as 2/3 node,
	// Then we have the following scenarios:
	// if our hash is the same with the majority, we blame the minority and the msg owner.
	// if our hash is the same with the one of the minorities, we blame the msg owner.
	blamePeers := make([]string, 0)
	hashToPeers := make(map[string][]string)
	ourHash := localCacheItem.Hash
	localCacheItem.lock.Lock()
	defer localCacheItem.lock.Unlock()
	for P2PID, hashValue := range localCacheItem.ConfirmedList {
		if peers, ok := hashToPeers[hashValue]; ok {
			peers = append(peers, P2PID)
			hashToPeers[hashValue] = peers
		} else {
			hashToPeers[hashValue] = []string{P2PID}
		}
	}

	threshold, err := GetThreshold(len(t.partyInfo.PartyIDMap))
	if err != nil {
		return nil, err
	}
	members := hashToPeers[ourHash]
	if len(members) < threshold {
		for key, peers := range hashToPeers {
			if key == ourHash {
				continue
			}
			// we blame all the rest of the minorities
			if len(peers) < threshold {
				blamePeers = append(blamePeers, peers...)
			}
		}
		// lastly, we add the data owner
		blamePeers = append(blamePeers, dataOwnerP2PID)
		return blamePeers, nil
	}

	for key, peers := range hashToPeers {
		if key == ourHash {
			continue
		}
		blamePeers = append(blamePeers, peers...)
	}
	// lastly, we add the data owner
	blamePeers = append(blamePeers, dataOwnerP2PID)
	return blamePeers, nil
}

func (t *TssCommon) getHashCheckBlamePeers(localCacheItem *LocalCacheItem, hashCheckErr error) ([]string, error) {
	// here we do the blame on the error on hash inconsistency
	// if we find the msg owner try to send the hash to us, we blame him and ignore the blame of the rest
	// of the other nodes, cause others may also be the victims.
	var blameP2PIDs []string

	dataOwner := localCacheItem.Msg.Routing.From
	dataOwnerP2PID, ok := t.PartyIDtoP2PID[dataOwner.Id]
	if !ok {
		t.logger.Warn().Msgf("error in find the data Owner P2PID\n")
		return nil, errors.New("error in find the data Owner P2PID")
	}
	switch hashCheckErr {
	case ErrHashFromOwner:
		blameP2PIDs = append(blameP2PIDs, dataOwnerP2PID.String())
		return blameP2PIDs, nil
	case ErrHashFromPeer:
		blameP2PIDs, err := t.findBlamePeers(localCacheItem, dataOwnerP2PID.String())
		return blameP2PIDs, err
	default:
		return nil, errors.New("unknown case")
	}
}

func (t *TssCommon) GetUnicastBlame(msgType string) ([]string, error) {
	peersID, ok := t.lastUnicastPeer[msgType]
	if !ok {
		t.logger.Error().Msg("fail to get the blamed peers")
		return nil, fmt.Errorf("fail to get the blamed peers %w", ErrTssTimeOut)
	}
	// use map to rule out the peer duplication
	peersMap := make(map[string]bool)
	for _, el := range peersID {
		peersMap[el.String()] = true
	}
	var onlinePeers []string
	for key, _ := range peersMap {
		onlinePeers = append(onlinePeers, key)
	}
	_, blamePeers, err := t.GetBlamePubKeysLists(onlinePeers)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to get the blamed peers")
		return nil, fmt.Errorf("fail to get the blamed peers %w", ErrTssTimeOut)
	}
	return blamePeers, nil
}

func (t *TssCommon) GetBroadcastBlame(lastMessageType string) ([]string, error) {
	localCachedItems := t.TryGetAllLocalCached()
	blamePeers, err := t.TssTimeoutBlame(localCachedItems, lastMessageType)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to get the blamed peers")
		return nil, fmt.Errorf("fail to get the blamed peers %w", ErrTssTimeOut)
	}
	return blamePeers, nil
}
