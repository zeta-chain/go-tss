package tss

import (
	"encoding/base64"
	"runtime/debug"
	"sort"
	"sync"
	"time"

	tsslibcommon "github.com/bnb-chain/tss-lib/common"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"

	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keysign"
	"github.com/zeta-chain/go-tss/keysign/ecdsa"
	"github.com/zeta-chain/go-tss/keysign/eddsa"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/go-tss/storage"
)

const msgSignatureGenerated = "signature generated"

func (t *Server) waitForSignatures(
	msgID, poolPubKey string,
	msgsToSign [][]byte,
	sigChan chan string,
) (keysign.Response, error) {
	defer func() {
		if r := recover(); r != nil {
			t.logger.Error().
				Str(logs.MsgID, msgID).
				Any("panic", r).
				Bytes("stack_trace", debug.Stack()).
				Msg("PANIC during waitForSignatures")
		}
	}()

	// TSS keysign include both form party and keysign itself, thus we wait twice of the timeout
	sigData, err := t.signatureNotifier.WaitForSignature(msgID, msgsToSign, poolPubKey, t.conf.KeySignTimeout, sigChan)
	if err != nil {
		return keysign.Response{}, errors.Wrap(err, "failed to wait for signatures")
	}

	return buildResponse(sigData, msgsToSign)
}

func (t *Server) generateSignature(
	msgID string,
	msgsToSign [][]byte,
	req keysign.Request,
	threshold int,
	allParticipants []string,
	localStateItem storage.KeygenLocalState,
	blameMgr *blame.Manager,
	keysignInstance keysign.TssKeySign,
	sigChan chan string,
) (keysign.Response, error) {
	defer func() {
		if r := recover(); r != nil {
			t.logger.Error().
				Str(logs.MsgID, msgID).
				Any("panic", r).
				Bytes("stack_trace", debug.Stack()).
				Msg("PANIC during generateSignature")
		}
	}()

	allPeersID, err := conversion.GetPeerIDsFromPubKeys(allParticipants)
	if err != nil {
		t.logger.Error().Msg("invalid block height or public key")
		return keysign.Response{
			Status: common.Fail,
			Blame:  blame.NewBlame(blame.InternalError, []blame.Node{}),
		}, nil
	}

	joinPartyStartTime := time.Now()

	onlinePeers, leader, errJoinParty := t.joinParty(
		msgID,
		req.BlockHeight,
		allParticipants,
		threshold,
		sigChan,
	)

	joinPartyTime := time.Since(joinPartyStartTime)

	if errJoinParty != nil {
		// we received the signature from waiting for signature
		if errors.Is(errJoinParty, p2p.ErrSigReceived) {
			return keysign.Response{}, errJoinParty
		}

		t.tssMetrics.KeysignJoinParty(joinPartyTime, false)

		var blameLeader blame.Blame
		leaderPubKey, err := conversion.GetPubKeyFromPeerID(leader.String())
		if err != nil {
			t.logger.Error().Err(errJoinParty).Msgf("fail to convert the peerID to public key %s", leader)
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{})
		} else {
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{{
				Pubkey:         leaderPubKey,
				BlameData:      nil,
				BlameSignature: nil,
			}})
		}

		t.broadcastKeysignFailure(msgID, allPeersID)

		// make sure we blame the leader as well
		t.logger.Error().
			Err(errJoinParty).
			Str(logs.MsgID, msgID).
			Str(logs.Peer, t.p2pCommunication.GetLocalPeerID()).
			Stringer(logs.Leader, leader).
			Any("peers_count", len(onlinePeers)).
			Any("peers", onlinePeers).
			Int64("block_height", req.BlockHeight).
			Any("blame_leader", blameLeader).
			Msg("Failed to form keysign party")

		return keysign.Response{
			Status: common.Fail,
			Blame:  blameLeader,
		}, nil
	}

	t.logger.Info().
		Str(logs.MsgID, msgID).
		Stringer(logs.Leader, leader).
		Float64(logs.Latency, joinPartyTime.Seconds()).
		Msg("Joined party for keysign")

	t.tssMetrics.KeysignJoinParty(joinPartyTime, true)

	isKeySignMember := false
	for _, el := range onlinePeers {
		if el == t.p2pCommunication.GetHost().ID() {
			isKeySignMember = true
		}
	}

	if !isKeySignMember {
		// we are not the keysign member so we quit keysign and waiting for signature
		t.logger.Info().
			Str(logs.MsgID, msgID).
			Stringer(logs.Host, t.p2pCommunication.GetHost().ID()).
			Msg("We are not the active signer")

		return keysign.Response{}, p2p.ErrNotActiveSigner
	}

	parsedPeers := make([]string, len(onlinePeers))
	for i, el := range onlinePeers {
		parsedPeers[i] = el.String()
	}

	signers, err := conversion.GetPubKeysFromPeerIDs(parsedPeers)
	if err != nil {
		sigChan <- msgSignatureGenerated

		return keysign.Response{
			Status: common.Fail,
			Blame:  blame.Blame{},
		}, nil
	}

	signatureData, err := keysignInstance.SignMessage(msgsToSign, localStateItem, signers)

	// the statistic of keygen only care about TSS it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("SignMessage failed")
		sigChan <- msgSignatureGenerated
		t.broadcastKeysignFailure(msgID, allPeersID)
		blameNodes := *blameMgr.GetBlame()
		return keysign.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil
	}

	sigChan <- msgSignatureGenerated

	// update signature notification
	if err := t.signatureNotifier.BroadcastSignature(msgID, signatureData, allPeersID); err != nil {
		return keysign.Response{}, errors.Wrap(err, "sig notifier: fail to broadcast signature")
	}

	return buildResponse(signatureData, msgsToSign)
}

func (t *Server) updateKeySignResult(result keysign.Response, timeSpent time.Duration) {
	t.tssMetrics.UpdateKeySign(timeSpent, result.Status == common.Success)
}

func (t *Server) KeySign(req keysign.Request) (keysign.Response, error) {
	// force party with a leader
	if req.Version != messages.VersionJoinPartyWithLeader {
		return keysign.Response{}, errors.Errorf("invalid version %q", req.Version)
	}

	msgID, err := req.MsgID()
	if err != nil {
		return keysign.Response{}, errors.Wrap(err, "unable to get message id")
	}

	t.logger.Info().Str(logs.MsgID, msgID).EmbedObject(&req).Msg("Keysign request")

	var keysignInstance keysign.TssKeySign
	var algo common.Algo
	pubKey, err := sdk.UnmarshalPubKey(sdk.AccPK, req.PoolPubKey)
	if err != nil {
		return keysign.Response{}, errors.Wrap(err, "unable to unmarshal pool pub key")
	}

	switch pubKey.Type() {
	case secp256k1.KeyType:
		algo = common.ECDSA
		keysignInstance = ecdsa.NewTssKeySign(
			t.p2pCommunication.GetLocalPeerID(),
			t.conf,
			t.p2pCommunication.BroadcastMsgChan,
			t.stopChan,
			msgID,
			t.privateKey,
			t.p2pCommunication,
			t.stateManager,
			len(req.Messages),
			t.logger,
		)
	case ed25519.KeyType:
		algo = common.EdDSA
		keysignInstance = eddsa.New(
			t.p2pCommunication.GetLocalPeerID(),
			t.conf,
			t.p2pCommunication.BroadcastMsgChan,
			t.stopChan,
			msgID,
			t.privateKey,
			t.p2pCommunication,
			t.stateManager,
			len(req.Messages),
			t.logger,
		)
	default:
		return keysign.Response{}, errors.New("invalid keysign algo")
	}

	keySignChannels := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(messages.TSSKeySignMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSKeySignVerMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSControlMsg, msgID, keySignChannels)
	t.p2pCommunication.SetSubscribe(messages.TSSTaskDone, msgID, keySignChannels)

	defer func() {
		t.p2pCommunication.CancelSubscribe(messages.TSSKeySignMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSKeySignVerMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)

		t.p2pCommunication.FreeStreams(msgID)
		t.partyCoordinator.FreeStreams(msgID)

		t.partyCoordinator.RemovePeerGroup(msgID)
	}()

	localStateItem, err := t.stateManager.GetLocalState(req.PoolPubKey)
	if err != nil {
		return keysign.Response{}, errors.Wrap(err, "stateManager: unable to get local keygen state")
	}

	var msgsToSign [][]byte
	for _, val := range req.Messages {
		msgToSign, err := base64.StdEncoding.DecodeString(val)
		if err != nil {
			return keysign.Response{}, errors.Wrapf(err, "unable to decode message %q", val)
		}

		msgsToSign = append(msgsToSign, msgToSign)
	}

	sort.SliceStable(msgsToSign, func(i, j int) bool {
		ma, err := common.MsgToHashInt(msgsToSign[i], algo)
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to convert the hash value")
			return false
		}
		mb, err := common.MsgToHashInt(msgsToSign[j], algo)
		if err != nil {
			t.logger.Error().Err(err).Msg("fail to convert the hash value")
			return false
		}

		return ma.Cmp(mb) >= 0
	})

	threshold, err := conversion.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		return keysign.Response{}, errors.New("fail to get threshold")
	}

	blameMgr := keysignInstance.GetTssCommonStruct().GetBlameMgr()

	var receivedSig, generatedSig keysign.Response
	var errWait, errGen error
	sigChan := make(chan string, 2)

	var wg sync.WaitGroup
	wg.Add(2)

	keysignStartTime := time.Now()

	// we wait for signatures
	go func() {
		defer wg.Done()

		receivedSig, errWait = t.waitForSignatures(msgID, req.PoolPubKey, msgsToSign, sigChan)
		switch {
		case errors.Is(errWait, p2p.ErrSigGenerated):
			// ok, we generate the signature ourselves
		case errWait != nil:
			t.logger.Error().
				Str(logs.MsgID, msgID).
				Err(errWait).
				Msg("Keysign: waitForSignatures failed")
		default:
			// we received an valid signature
			sigChan <- p2p.NotificationSigReceived
			t.logger.Debug().
				Str(logs.MsgID, msgID).
				Stringer(logs.Peer, receivedSig.Blame).
				Msg("Keysign:received signature")
		}
	}()

	// we generate the signature ourselves
	go func() {
		defer wg.Done()
		generatedSig, errGen = t.generateSignature(
			msgID,
			msgsToSign,
			req,
			threshold,
			localStateItem.ParticipantKeys,
			localStateItem,
			blameMgr,
			keysignInstance,
			sigChan,
		)
	}()
	wg.Wait()
	close(sigChan)
	keysignTime := time.Since(keysignStartTime)
	// we received the generated verified signature, so we return
	if errWait == nil {
		t.updateKeySignResult(receivedSig, keysignTime)
		return receivedSig, nil
	}
	// for this round, we are not the active signer
	if errors.Is(errGen, p2p.ErrSigReceived) || errors.Is(errGen, p2p.ErrNotActiveSigner) {
		t.updateKeySignResult(receivedSig, keysignTime)
		return receivedSig, nil
	}
	// we get the signature from our tss keysign
	t.updateKeySignResult(generatedSig, keysignTime)
	return generatedSig, errGen
}

func (t *Server) broadcastKeysignFailure(messageID string, peers []peer.ID) {
	if err := t.signatureNotifier.BroadcastFailed(messageID, peers); err != nil {
		t.logger.Err(err).Msg("fail to broadcast keysign failure")
	}
}

var base64enc = base64.StdEncoding.EncodeToString

func buildResponse(sigs []*tsslibcommon.SignatureData, msgsToSign [][]byte) (keysign.Response, error) {
	if len(sigs) == 0 {
		return keysign.Response{}, errors.New("empty signatures list")
	}

	signatures := make([]keysign.Signature, len(sigs))

	for i, sig := range sigs {
		signatures[i] = keysign.NewSignature(
			base64enc(msgsToSign[i]),
			base64enc(sig.R),
			base64enc(sig.S),
			base64enc(sig.SignatureRecovery),
		)
	}

	return keysign.NewResponse(signatures, common.Success, blame.Blame{}), nil
}
