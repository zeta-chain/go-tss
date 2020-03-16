package tss

import (
	"encoding/base64"
	"errors"
	"fmt"
	"strings"
	"sync/atomic"

	"github.com/libp2p/go-libp2p-core/peer"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) KeySign(req keysign.Request) (keysign.Response, error) {
	t.logger.Info().Str("pool pub key", req.PoolPubKey).
		Str("signer pub keys", strings.Join(req.SignerPubKeys, ",")).
		Str("msg", req.Message).
		Msg("received keysign request")
	emptyResp := keysign.Response{}
	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return emptyResp, err
	}

	keysignInstance := keysign.NewTssKeySign(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		&t.Status.CurrKeySign,
		msgID,
	)

	keySignChannels := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignVerMsg, msgID, keySignChannels)

	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignVerMsg, msgID)

	localStateItem, err := t.stateManager.GetLocalState(req.PoolPubKey)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to get local keygen state: %w", err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	if len(req.SignerPubKeys) == 0 {
		return emptyResp, errors.New("empty signer pub keys")
	}

	threshold, err := common.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to get the threshold")
		return emptyResp, errors.New("fail to get threshold")
	}
	if len(req.SignerPubKeys) <= threshold {
		t.logger.Error().Msgf("not enough signers, threshold=%d and signers=%d", threshold, len(req.SignerPubKeys))
		return emptyResp, errors.New("not enough signers")
	}

	if !t.isPartOfKeysignParty(req.SignerPubKeys) {
		// local node is not party of the keysign , wait for signature
		peerIDs, err := GetPeerIDs(req.SignerPubKeys)
		if err != nil {
			return emptyResp, fmt.Errorf("fail to convert pub key to peer id: %w", err)
		}

		data, err := t.signatureNotifier.WaitForSignature(msgID, peerIDs, t.conf.KeySignTimeout)
		if err != nil {
			return emptyResp, fmt.Errorf("fail to get signature:%w", err)
		}
		if data == nil || (len(data.S) == 0 && len(data.R) == 0) {
			return emptyResp, errors.New("keysign failed")
		}
		return keysign.NewResponse(
			base64.StdEncoding.EncodeToString(data.R),
			base64.StdEncoding.EncodeToString(data.S),
			common.Success,
			common.NoBlame,
		), nil
	}
	// get all the tss nodes that were part of the original key gen
	signers, err := GetPeerIDs(localStateItem.ParticipantKeys)
	if err != nil {
		return emptyResp, fmt.Errorf("fail to convert pub keys to peer id:%w", err)
	}

	result, leaderPeerID, err := t.joinParty(msgID, msgToSign, req.SignerPubKeys)
	if err != nil {
		// just blame the leader node
		pKey, err := GetPubKeyFromPeerID(leaderPeerID.String())
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to extract pub key from peer ID")
		}
		t.broadcastKeysignFailure(msgID, signers)
		return keysign.Response{
			Status: common.Fail,
			Blame:  common.NewBlame(common.BlameTssTimeout, []string{pKey}),
		}, fmt.Errorf("fail to form keysign party: %s", result.Type)
	}
	if result.Type != messages.JoinPartyResponse_Success {
		pKey, err := GetPubKeyFromPeerID(leaderPeerID.String())
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to extract pub key from peer ID")
		}
		blame, err := t.getBlamePeers(req.SignerPubKeys, result.PeerIDs)
		if err != nil {
			t.logger.Err(err).Msg("fail to get peers to blame")
		}
		t.broadcastKeysignFailure(msgID, signers)
		// make sure we blame the leader as well
		blame.AddBlameNodes(pKey)
		return keysign.Response{
			Status: common.Fail,
			Blame:  blame,
		}, fmt.Errorf("fail to form keysign party: %s", result.Type)
	}

	signatureData, err := keysignInstance.SignMessage(msgToSign, localStateItem, req.SignerPubKeys)
	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keysign")
		atomic.AddUint64(&t.Status.FailedKeySign, 1)
		t.broadcastKeysignFailure(msgID, signers)
		return keysign.Response{
			Status: common.Fail,
			Blame:  keysignInstance.GetTssCommonStruct().BlamePeers,
		}, nil
	}

	atomic.AddUint64(&t.Status.SucKeySign, 1)

	// update signature notification
	if err := t.signatureNotifier.BroadcastSignature(msgID, signatureData, signers); err != nil {
		return emptyResp, fmt.Errorf("fail to broadcast signature:%w", err)
	}
	return keysign.NewResponse(
		base64.StdEncoding.EncodeToString(signatureData.R),
		base64.StdEncoding.EncodeToString(signatureData.S),
		common.Success,
		common.NoBlame,
	), nil
}

func (t *TssServer) broadcastKeysignFailure(messageID string, peers []peer.ID) {
	if err := t.signatureNotifier.BroadcastFailed(messageID, peers); err != nil {
		t.logger.Err(err).Msg("fail to broadcast keysign failure")
	}
}

func (t *TssServer) isPartOfKeysignParty(parties []string) bool {
	for _, item := range parties {
		if t.localNodePubKey == item {
			return true
		}
	}
	return false
}
