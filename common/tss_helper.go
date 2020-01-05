package common

import (
	"crypto/elliptic"
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"math"
	"math/big"
	"os"

	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/btcd/btcec"
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
		if nil != err {
			return err
		}
		partyIDtoP2PID[id] = peerID
	}
	return nil
}

func MsgToHashInt(msg []byte) (*big.Int, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if nil != err {
		return nil, fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hashToInt(h.Sum(nil), btcec.S256()), nil
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

func InitLog(level string, pretty bool) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("%s is not a valid log-level, falling back to 'info'", level)
	}
	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	zerolog.SetGlobalLevel(l)
	log.Logger = log.Output(out).With().Str("service", "go-tss-test").Logger()
}

// NodeSyncGetBlame handles the blame caused by the nodesync error
func (t *TssCommon) GetBlameNodesPublicKeys(peers []string, option bool) ([]string, error) {
	var result []string
	blamePubKeys := make([]string, 0)
	localPartyInfo := t.getPartyInfo()
	partyIDMap := localPartyInfo.PartyIDMap

	switch option {
	// if option is true, we convert nodes (in the peers list) P2PID to public key
	case true:
		for partyID, p2pID := range t.PartyIDtoP2PID {
			for _, el := range peers {
				if el == p2pID.String() {
					result = append(result, partyID)
				}
			}
		}
		// if option is false, we convert nodes (NOT in the peers list) P2PID to public key
	default:
		for partyID, p2pID := range t.PartyIDtoP2PID {
			found := false
			for _, each := range peers {
				if p2pID.String() == each {
					found = true
				}
			}
			if found == false {
				result = append(result, partyID)
			}
		}
	}
	for _, partyID := range result {
		blameParty, ok := partyIDMap[partyID]
		if !ok {
			t.logger.Error().Msgf("cannot find the blame party")
			return nil, fmt.Errorf("cannot find the blame party")
		}
		blamePartyKeyBytes := blameParty.GetKey()
		var pk secp256k1.PubKeySecp256k1
		copy(pk[:], blamePartyKeyBytes)
		blamedPubKey, err := sdk.Bech32ifyAccPub(pk)
		if err != nil {
			t.logger.Error().Msgf("error in decode the pub key")
			return nil, err
		}
		blamePubKeys = append(blamePubKeys, blamedPubKey)
	}

	return blamePubKeys, nil
}

// TssTimeoutBlame handles the blame caused by the nodesync error
// We believe the node itself will not cheat himself, so we go through
// the confirmed list to find out the absent node(s) that fail to send the
// hash of the message. The node who receive the broadcast message must send
// the VerMsg otherwise, we blame them
func (t *TssCommon) TssTimeoutBlame(localCachedItems []*LocalCacheItem) ([]string, error) {
	var sumStandbyPeers []string
	for _, el := range localCachedItems {
		if len(t.P2PPeers) == el.TotalConfirmParty() {
			continue
		}
		peers := el.GetPeers()
		sumStandbyPeers = append(sumStandbyPeers, peers[:]...)
	}
	blamePeers, err := t.GetBlameNodesPublicKeys(sumStandbyPeers, false)
	if err != nil {
		return nil, err
	}
	return blamePeers, nil
}

func (t *TssCommon) findBlamePeers(localCacheItem *LocalCacheItem, dataOwnerP2PID string) ([]string, error) {
	// our tss is based on the assumption that 2/3 of the nodes are honest. we define the majority as 2/3 node,
	//Then we have the following scenarios:
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
	members, _ := hashToPeers[ourHash]
	switch {
	case len(members) < threshold:
		for key, peers := range hashToPeers {
			if key == ourHash {
				continue
			}
			//we blame all the rest of the minorities
			if len(peers) < threshold {
				blamePeers = append(blamePeers, peers[:]...)
			}
		}
		// lastly, we add the data owner
		blamePeers = append(blamePeers, dataOwnerP2PID)
		return blamePeers, nil
	default:
		for key, peers := range hashToPeers {
			if key == ourHash {
				continue
			}
			blamePeers = append(blamePeers, peers[:]...)
		}
		// lastly, we add the data owner
		blamePeers = append(blamePeers, dataOwnerP2PID)
		return blamePeers, nil
	}
}

func (t *TssCommon) getHashCheckBlame(localCacheItem *LocalCacheItem, err error) ([]string, error) {
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
	switch err {
	case ErrHashFromOwner:
		blameP2PIDs = append(blameP2PIDs, dataOwnerP2PID.String())
		return blameP2PIDs, err
	case ErrHashFromPeer:
		blameP2PIDs, err = t.findBlamePeers(localCacheItem, dataOwnerP2PID.String())
		return blameP2PIDs, err
	default:
		return nil, errors.New("unknown case")
	}
}
