package tss

import (
	"encoding/base64"
	"errors"
	"fmt"
	"path/filepath"
	"sort"
	"sync/atomic"

	"github.com/binance-chain/tss-lib/ecdsa/signing"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) KeySign(req keysign.KeySignReq) (keysign.KeySignResp, error) {
	t.tssKeySignLocker.Lock()
	defer t.tssKeySignLocker.Unlock()

	keySignFlag := common.Success

	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return keysign.KeySignResp{}, err
	}

	keysignInstance := keysign.NewTssKeySign(
		t.homeBase,
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.p2pCommunication.BroadcastMsgChan,
		&t.stopChan,
		&t.Status.CurrKeySign,
		msgID,
	)

	keygenMsgChannel := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignVerMsg, msgID, keygenMsgChannel)

	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignVerMsg, msgID)
	localStateItem, err := t.getLocalState(req.PoolPubKey)
	if err != nil {
		return keysign.KeySignResp{}, fmt.Errorf("fail to get local keygen state: %w", err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if err != nil {
		return keysign.KeySignResp{}, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	result, err := t.joinParty(msgID, msgToSign, localStateItem)
	if err != nil {
		// don't blame node for forming party
		return keysign.KeySignResp{}, fmt.Errorf("fail to form keysign party: %w", err)
	}
	if result.Type != messages.JoinPartyResponse_Success {
		return keysign.KeySignResp{}, fmt.Errorf("fail to form keysign party: %s", result.Type)
	}
	keys, err := GetPubKeysFromPeerIDs(result.PeerID)
	if err != nil {
		return keysign.KeySignResp{}, fmt.Errorf("fail to convert peer ID to pub keys: %w", err)
	}
	signatureData, err := keysignInstance.SignMessage(msgToSign, localStateItem, keys)
	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keysign")
		atomic.AddUint64(&t.Status.FailedKeySign, 1)
		keySignFlag = common.Fail
		signatureData = &signing.SignatureData{}
	} else {
		atomic.AddUint64(&t.Status.SucKeySign, 1)
	}
	blame := keysignInstance.GetTssCommonStruct().BlamePeers

	// this indicates we are not in this round keysign
	if signatureData == nil && err == nil {
		return keysign.NewKeySignResp("", "", common.NA, blame), nil
	}

	return keysign.NewKeySignResp(
		base64.StdEncoding.EncodeToString(signatureData.R),
		base64.StdEncoding.EncodeToString(signatureData.S),
		keySignFlag,
		blame,
	), nil
}

func (t *TssServer) getLocalState(poolPubKey string) (common.KeygenLocalStateItem, error) {
	localStateItem := common.KeygenLocalStateItem{}
	if len(poolPubKey) == 0 {
		return localStateItem, errors.New("pool pub key is empty")
	}
	localFileName := fmt.Sprintf("localstate-%s.json", poolPubKey)
	if len(t.homeBase) > 0 {
		localFileName = filepath.Join(t.homeBase, localFileName)
	}
	localStateItem, err := common.LoadLocalState(localFileName)
	if err != nil {
		return localStateItem, fmt.Errorf("fail to read local state file: %w", err)
	}
	return localStateItem, nil
}

func (t *TssServer) joinParty(msgID string, messageToSign []byte, localStateItem common.KeygenLocalStateItem) (*messages.JoinPartyResponse, error) {
	keys := localStateItem.ParticipantKeys
	sort.Slice(keys, func(i, j int) bool {
		return keys[i] < keys[j]
	})
	peerIDs, err := GetPeerIDsFromPubKeys(keys)
	if err != nil {
		return nil, fmt.Errorf("fail to convert pub key to peer id: %w", err)
	}
	totalNodes := len(localStateItem.ParticipantKeys)
	threshold, err := common.GetThreshold(totalNodes)
	if err != nil {
		return nil, err
	}
	leader, err := p2p.LeaderNode(messageToSign, int32(totalNodes))
	if err != nil {
		return nil, fmt.Errorf("fail to get leader node")
	}

	leaderPeerID, err := GetPeerIDFromPubKey(keys[leader])
	if err != nil {
		return nil, fmt.Errorf("fail to get peer id from node pubkey: %w", err)
	}
	joinPartyReq := &messages.JoinPartyRequest{
		ID:        msgID,
		Threshold: int32(threshold + 1),
		PeerID:    peerIDs,
	}
	return t.partyCoordinator.JoinParty(leaderPeerID, joinPartyReq)
}
