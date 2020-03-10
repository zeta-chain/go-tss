package tss

import (
	"fmt"
	"strings"
	"sync/atomic"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

// getBlamePeers is to find out which node to blame
// keys is the node pub key of the nodes that are supposed to be online
// onlinePeers is a slice of peer id that actually online
// this method is to find out the gap
func (t *TssServer) getBlamePeers(keys []string, onlinePeers []string) (common.Blame, error) {
	blame := common.Blame{
		FailReason: common.BlameTssTimeout,
	}
	for _, item := range keys {
		found := false
		peerID, err := GetPeerIDFromPubKey(item)
		if err != nil {
			return blame, fmt.Errorf("fail to get peer id from pub key")
		}
		for _, p := range onlinePeers {
			if strings.EqualFold(peerID.String(), p) {
				found = true
				break
			}
		}
		if !found {
			blame.BlameNodes = append(blame.BlameNodes, item)
		}
	}
	return blame, nil
}

func (t *TssServer) Keygen(req keygen.Request) (keygen.Response, error) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()

	status := common.Success

	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return keygen.Response{}, err
	}

	keygenInstance := keygen.NewTssKeyGen(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.localNodePubKey,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		t.preParams,
		&t.Status.CurrKeyGen,
		msgID,
		t.stateManager)

	keygenMsgChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenVerMsg, msgID, keygenMsgChannel)

	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenVerMsg, msgID)
	result, leaderPeerID, err := t.joinParty(msgID, []byte(strings.Join(req.Keys, ",")), req.Keys)
	if err != nil {

		blame, err := t.getBlamePeers(req.Keys, []string{leaderPeerID.String()})
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}

		return keygen.Response{
			Status: common.Fail,
			Blame:  blame,
		}, fmt.Errorf("fail to form keysign party: %s", result.Type)
	}

	if result.Type != messages.JoinPartyResponse_Success {
		blamePeers := append(result.PeerIDs, leaderPeerID.String())
		blame, err := t.getBlamePeers(req.Keys, blamePeers)
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}
		return keygen.Response{
			Status: common.Fail,
			Blame:  blame,
		}, fmt.Errorf("fail to form keygen party: %s", result.Type)
	}
	t.logger.Info().Msg("keygen party formed")
	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(req)
	if err != nil {
		atomic.AddUint64(&t.Status.FailedKeyGen, 1)
		t.logger.Error().Err(err).Msg("err in keygen")
		return keygen.NewResponse("", "", common.Fail, keygenInstance.GetTssCommonStruct().BlamePeers), err
	} else {
		atomic.AddUint64(&t.Status.SucKeyGen, 1)
	}

	newPubKey, addr, err := common.GetTssPubKey(k)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to generate the new Tss key")
		status = common.Fail
	}

	return keygen.NewResponse(
		newPubKey,
		addr.String(),
		status,
		keygenInstance.GetTssCommonStruct().BlamePeers,
	), nil
}
