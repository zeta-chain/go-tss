package blame

import (
	btss "github.com/bnb-chain/tss-lib/tss"
	mapset "github.com/deckarep/golang-set"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"

	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/messages"
)

func (m *Manager) tssTimeoutBlame(lastMessageType string, partyIDMap map[string]*btss.PartyID) ([]string, error) {
	standbyNodes := m.roundMgr.GetByRound(lastMessageType)
	if len(standbyNodes) == 0 {
		return nil, nil
	}

	standbySet := mapset.NewSet()
	for _, v := range standbyNodes {
		standbySet.Add(v)
	}

	peersSet := mapset.NewSet()
	for _, el := range partyIDMap {
		if el.Id != m.localPartyID {
			peersSet.Add(el.Id)
		}
	}

	var blames []string
	diff := peersSet.Difference(standbySet).ToSlice()
	for _, el := range diff {
		blames = append(blames, el.(string))
	}

	blamePubKeys, err := conversion.AccPubKeysFromPartyIDs(blames, m.partyInfo.PartyIDMap)
	if err != nil {
		return nil, errors.Wrap(err, "unable to derive blame public keys")
	}

	return blamePubKeys, nil
}

// this blame blames the node who cause the timeout in node sync
func (m *Manager) NodeSyncBlame(keys []string, onlinePeers []peer.ID) (Blame, error) {
	blame := NewBlame(TssSyncFail, nil)

	for _, item := range keys {
		found := false
		peerID, err := conversion.GetPeerIDFromPubKey(item)
		if err != nil {
			return blame, errors.Wrap(err, "unable to get peer id from pub key")
		}

		for _, p := range onlinePeers {
			if p == peerID {
				found = true
				break
			}
		}

		if !found {
			blame.BlameNodes = append(blame.BlameNodes, NewNode(item, nil, nil))
		}
	}

	return blame, nil
}

// this blame blames the node who cause the timeout in unicast message
func (m *Manager) GetUnicastBlame(lastMsgType string) ([]Node, error) {
	m.lastMsgLocker.RLock()

	if len(m.lastUnicastPeer) == 0 {
		m.lastMsgLocker.RUnlock()
		m.logger.Debug().Msg("we do not have any unicast message received yet")
		return nil, nil
	}

	peersMap := make(map[string]bool)
	peersID, ok := m.lastUnicastPeer[lastMsgType]
	m.lastMsgLocker.RUnlock()

	if !ok {
		return nil, errors.Wrap(ErrTimeoutTSS, "fail to find peers of the given msg type")
	}

	for _, el := range peersID {
		peersMap[el.String()] = true
	}

	var onlinePeers []string
	for key := range peersMap {
		onlinePeers = append(onlinePeers, key)
	}

	_, blamePeers, err := m.GetBlamePubKeysLists(onlinePeers)
	if err != nil {
		return nil, errors.Wrap(err, "unable to get the blamed peers")
	}

	var blameNodes []Node
	for _, el := range blamePeers {
		blameNodes = append(blameNodes, NewNode(el, nil, nil))
	}

	return blameNodes, nil
}

// this blame blames the node who cause the timeout in broadcast message
func (m *Manager) GetBroadcastBlame(lastMessageType string) ([]Node, error) {
	blamePeers, err := m.tssTimeoutBlame(lastMessageType, m.partyInfo.PartyIDMap)
	if err != nil {
		return nil, errors.Wrap(err, "tssTimeoutBlame")
	}

	var blameNodes []Node
	for _, el := range blamePeers {
		blameNodes = append(blameNodes, NewNode(el, nil, nil))
	}

	return blameNodes, nil
}

// TSSWrongShareBlame blames the node who provide the wrong share
func (m *Manager) TSSWrongShareBlame(wiredMsg *messages.WireMessage) (string, error) {
	shareOwner := wiredMsg.Routing.From
	owner, ok := m.partyInfo.PartyIDMap[shareOwner.Id]
	if !ok {
		return "", errors.New("unable to find the share owner")
	}

	pk, err := conversion.PartyIDtoPubKey(owner)
	if err != nil {
		return "", errors.Wrap(err, "unable to convert party id to pub key")
	}

	return pk, nil
}

// TSSMissingShareBlame blames the node fail to send the shares to the node
// with batch signing, we need to put the accepted shares into different message group
// then search the missing share for each keysign message.
func (m *Manager) TSSMissingShareBlame(rounds int, algo messages.Algo) ([]Node, bool, error) {
	var (
		acceptedShareForMsg = make(map[string][][]string)
		blameNodes          []Node
		peers               []string
		isUnicast           bool
	)

	m.acceptShareLocker.Lock()
	for roundInfo, value := range m.acceptedShares {
		cachedShares, ok := acceptedShareForMsg[roundInfo.MsgIdentifier]
		if !ok {
			cachedShares := make([][]string, rounds)
			cachedShares[roundInfo.Index] = value
			acceptedShareForMsg[roundInfo.MsgIdentifier] = cachedShares
			continue
		}

		// should not happen
		if roundInfo.Index >= len(cachedShares) {
			m.logger.Error().
				Int("round_index", roundInfo.Index).
				Int("cached_shares_len", len(cachedShares)).
				Int("rounds", rounds).
				Int("algo", int(algo)).
				Msg("Unexpected round index")

			continue
		}

		cachedShares[roundInfo.Index] = value
	}
	m.acceptShareLocker.Unlock()

	for _, cachedShares := range acceptedShareForMsg {
		// we search from the first round to find the missing
		for index, el := range cachedShares {
			if len(el)+1 == len(m.PartyIDtoP2PID) {
				continue
			}

			switch algo {
			// we find whether the missing share is in unicast
			case messages.ECDSAKEYGEN:
				// we are processing the keygen and if the missing shares is in second round(index=1)
				// we mark it as the unicast.
				if index == 1 {
					isUnicast = true
				}
			case messages.ECDSAKEYSIGN:
				// we are processing the keysign and if the missing shares is in the 5 round(index<1)
				// we all mark it as the unicast, because in some cases, the error will be detected
				// in the following round, so we cannot "trust" the node stops at the current round.
				if index < 5 {
					isUnicast = true
				}

			case messages.EDDSAKEYGEN:
				if index == 2 {
					isUnicast = true
				}
			case messages.EDDSAKEYSIGN:
				// currently, EDDSA do not have proof, so all the communication is broadcast.
				isUnicast = false

			default:
				m.logger.Error().
					Msgf("fail to find the algorithm for this keygen/keysign, set unicast as false by default")
				isUnicast = false
			}

			// we add our own id to avoid blame ourselves
			// since all the local parties have the same id, so we just need to take one of them to get the peer

			el = append(el, m.localPartyID)
			for _, pid := range el {
				peers = append(peers, m.PartyIDtoP2PID[pid].String())
			}
			break
		}
		blamePubKeys, err := m.getBlamePubKeysNotInList(peers)
		if err != nil {
			return nil, isUnicast, errors.Wrap(err, "getBlamePubKeysNotInList")
		}

		for _, el := range blamePubKeys {
			node := Node{
				el,
				nil,
				nil,
			}
			blameNodes = append(blameNodes, node)
		}
	}
	return blameNodes, isUnicast, nil
}
