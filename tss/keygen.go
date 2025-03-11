package tss

import (
	"errors"
	"time"

	"github.com/bnb-chain/tss-lib/crypto"
	"github.com/cosmos/cosmos-sdk/types"
	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/keygen/ecdsa"
	"github.com/zeta-chain/go-tss/keygen/eddsa"
	"github.com/zeta-chain/go-tss/messages"
)

func (t *TssServer) Keygen(req keygen.Request) (keygen.Response, error) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()
	status := common.Success
	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return keygen.Response{}, err
	}

	var keygenInstance keygen.TssKeyGen
	switch req.Algo {
	case common.ECDSA:
		keygenInstance = ecdsa.NewTssKeyGen(
			t.p2pCommunication.GetLocalPeerID(),
			t.conf,
			t.localNodePubKey,
			t.p2pCommunication.BroadcastMsgChan,
			t.stopChan,
			t.preParams,
			msgID,
			t.stateManager,
			t.privateKey,
			t.p2pCommunication)
	case common.EdDSA:
		keygenInstance = eddsa.NewTssKeyGen(
			t.p2pCommunication.GetLocalPeerID(),
			t.conf,
			t.localNodePubKey,
			t.p2pCommunication.BroadcastMsgChan,
			t.stopChan,
			msgID,
			t.stateManager,
			t.privateKey,
			t.p2pCommunication)
	default:
		return keygen.Response{}, errors.New("invalid keygen algo")
	}

	keygenMsgChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(messages.TSSKeyGenMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSKeyGenVerMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSControlMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(messages.TSSTaskDone, msgID, keygenMsgChannel)

	defer func() {
		t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenVerMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
		t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)

		t.p2pCommunication.ReleaseStream(msgID)
		t.partyCoordinator.ReleaseStream(msgID)
	}()
	sigChan := make(chan string)
	blameMgr := keygenInstance.GetTssCommonStruct().GetBlameMgr()
	joinPartyStartTime := time.Now()
	onlinePeers, leader, errJoinParty := t.joinParty(
		msgID,
		req.Version,
		req.BlockHeight,
		req.Keys,
		len(req.Keys)-1,
		sigChan,
	)
	joinPartyTime := time.Since(joinPartyStartTime)
	if errJoinParty != nil {
		t.logger.Error().
			Err(errJoinParty).
			Msgf("failed to joinParty after %s, onlinePeers=%v", joinPartyTime, onlinePeers)

		t.tssMetrics.KeygenJoinParty(joinPartyTime, false)
		t.tssMetrics.UpdateKeyGen(0, false)
		// this indicate we are processing the leaderless join party
		if leader == "NONE" {
			if onlinePeers == nil {
				t.logger.Error().Err(err).Msg("error before we start join party")
				return keygen.Response{
					Status: common.Fail,
					Blame:  blame.NewBlame(blame.InternalError, []blame.Node{}),
				}, nil
			}
			blameNodes, err := blameMgr.NodeSyncBlame(req.Keys, onlinePeers)
			if err != nil {
				t.logger.Err(errJoinParty).Msg("fail to get peers to blame")
			}
			// make sure we blame the leader as well
			t.logger.Error().Err(errJoinParty).Msgf("fail to form keygen party with online:%v", onlinePeers)
			return keygen.Response{
				Status: common.Fail,
				Blame:  blameNodes,
			}, nil
		}

		var blameLeader blame.Blame
		var blameNodes blame.Blame
		blameNodes, err = blameMgr.NodeSyncBlame(req.Keys, onlinePeers)
		if err != nil {
			t.logger.Error().Err(err).Msg("failed to blame nodes for joinParty failure")
		}
		leaderPubKey, err := conversion.GetPubKeyFromPeerID(leader)
		if err != nil {
			t.logger.Error().Err(err).Msgf("failed to convert peerID->pubkey for leader %s", leader)
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{})
		} else {
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{{Pubkey: leaderPubKey, BlameData: nil, BlameSignature: nil}})
		}

		if len(onlinePeers) != 0 {
			t.logger.Trace().Msgf("there were %d onlinePeers, adding leader to %d existing nodes blamed",
				len(onlinePeers), len(blameNodes.BlameNodes))
			blameNodes.AddBlameNodes(blameLeader.BlameNodes...)
		} else {
			t.logger.Trace().Msgf("there were %d onlinePeers, setting blame nodes to just the leader",
				len(onlinePeers))
			blameNodes = blameLeader
		}
		t.logger.Error().Err(errJoinParty).Msgf("fail to form keygen party with online:%v", onlinePeers)

		return keygen.Response{
			Status: common.Fail,
			Blame:  blameNodes,
		}, nil
	}

	t.logger.Info().Msg("joinParty succeeded, keygen party formed")
	t.notifyJoinPartyChan()
	t.tssMetrics.KeygenJoinParty(joinPartyTime, true)

	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	beforeKeygen := time.Now()
	k, err := keygenInstance.GenerateNewKey(req)
	keygenTime := time.Since(beforeKeygen)
	if err != nil {
		t.tssMetrics.UpdateKeyGen(keygenTime, false)
		blameNodes := *blameMgr.GetBlame()
		t.logger.Error().Err(err).Msgf("failed to generate key, blaming: %+v", blameNodes.BlameNodes)
		return keygen.NewResponse(common.ECDSA, "", "", common.Fail, blameNodes), err
	} else {
		t.tssMetrics.UpdateKeyGen(keygenTime, true)
	}

	var newPubKey string
	var addr types.AccAddress
	switch req.Algo {
	case common.ECDSA:
		newPubKey, addr, err = conversion.GetTssPubKeyECDSA(k)
	case common.EdDSA:
		newPubKey, addr, err = conversion.GetTssPubKeyEDDSA(k)
	default:
		newPubKey, addr, err = conversion.GetTssPubKeyECDSA(k)
	}
	if err != nil {
		t.logger.Error().Err(err).Msg("failed to generate new tss pubkey from generated key")
		status = common.Fail
	}

	blameNodes := *blameMgr.GetBlame()
	t.logger.Trace().Msgf("returning from keygen with status=%d, blaming=%+v", status, blameNodes.BlameNodes)
	return keygen.NewResponse(
		common.ECDSA,
		newPubKey,
		addr.String(),
		status,
		blameNodes,
	), nil
}

func (t *TssServer) KeygenAllAlgo(req keygen.Request) ([]keygen.Response, error) {
	// this is the algo we currently support
	algos := []common.Algo{common.ECDSA, common.EdDSA}
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()
	status := common.Success
	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return nil, err
	}

	ecdsaKeygenInstance := ecdsa.NewTssKeyGen(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.localNodePubKey,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		t.preParams,
		msgID+string(common.ECDSA),
		t.stateManager,
		t.privateKey,
		t.p2pCommunication)

	eddsaKeygenInstance := eddsa.NewTssKeyGen(
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.localNodePubKey,
		t.p2pCommunication.BroadcastMsgChan,
		t.stopChan,
		msgID+string(common.EdDSA),
		t.stateManager,
		t.privateKey,
		t.p2pCommunication)
	_ = eddsaKeygenInstance
	_ = ecdsaKeygenInstance
	keygenInstances := make(map[common.Algo]keygen.TssKeyGen)
	keygenInstances[common.ECDSA] = ecdsaKeygenInstance
	keygenInstances[common.EdDSA] = eddsaKeygenInstance

	for algo, instance := range keygenInstances {
		msgID := msgID + string(algo)
		keygenMsgChannel := instance.GetTssKeyGenChannels()
		t.p2pCommunication.SetSubscribe(messages.TSSKeyGenMsg, msgID, keygenMsgChannel)
		t.p2pCommunication.SetSubscribe(messages.TSSKeyGenVerMsg, msgID, keygenMsgChannel)
		t.p2pCommunication.SetSubscribe(messages.TSSControlMsg, msgID, keygenMsgChannel)
		t.p2pCommunication.SetSubscribe(messages.TSSTaskDone, msgID, keygenMsgChannel)

		defer func() {
			t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenMsg, msgID)
			t.p2pCommunication.CancelSubscribe(messages.TSSKeyGenVerMsg, msgID)
			t.p2pCommunication.CancelSubscribe(messages.TSSControlMsg, msgID)
			t.p2pCommunication.CancelSubscribe(messages.TSSTaskDone, msgID)

			t.p2pCommunication.ReleaseStream(msgID)
			t.partyCoordinator.ReleaseStream(msgID)
		}()
	}
	sigChan := make(chan string)
	// since all the keygen algorithms share the join party, so we need to use the ecdsa algo's blame manager
	blameMgr := keygenInstances[common.ECDSA].GetTssCommonStruct().GetBlameMgr()
	joinPartyStartTime := time.Now()
	onlinePeers, leader, errJoinParty := t.joinParty(
		msgID,
		req.Version,
		req.BlockHeight,
		req.Keys,
		len(req.Keys)-1,
		sigChan,
	)
	joinPartyTime := time.Since(joinPartyStartTime)
	if errJoinParty != nil {
		t.tssMetrics.KeygenJoinParty(joinPartyTime, false)
		t.tssMetrics.UpdateKeyGen(0, false)
		// this indicate we are processing the leaderless join party
		if leader == "NONE" {
			if onlinePeers == nil {
				t.logger.Error().Err(err).Msg("error before we start join party")
				return []keygen.Response{{
					Status: common.Fail,
					Blame:  blame.NewBlame(blame.InternalError, []blame.Node{}),
				}}, nil
			}
			blameNodes, err := blameMgr.NodeSyncBlame(req.Keys, onlinePeers)
			if err != nil {
				t.logger.Err(errJoinParty).Msg("fail to get peers to blame")
			}
			// make sure we blame the leader as well
			t.logger.Error().Err(errJoinParty).Msgf("fail to form keygen party with online:%v", onlinePeers)
			return []keygen.Response{{
				Status: common.Fail,
				Blame:  blameNodes,
			}}, nil
		}

		var blameLeader blame.Blame
		var blameNodes blame.Blame
		blameNodes, err = blameMgr.NodeSyncBlame(req.Keys, onlinePeers)
		if err != nil {
			t.logger.Err(errJoinParty).Msg("fail to get peers to blame")
		}
		leaderPubKey, err := conversion.GetPubKeyFromPeerID(leader)
		if err != nil {
			t.logger.Error().Err(errJoinParty).Msgf("fail to convert the peerID to public key with leader %s", leader)
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{})
		} else {
			blameLeader = blame.NewBlame(blame.TssSyncFail, []blame.Node{{
				Pubkey:         leaderPubKey,
				BlameData:      nil,
				BlameSignature: nil,
			}})
		}
		if len(onlinePeers) != 0 {
			blameNodes.AddBlameNodes(blameLeader.BlameNodes...)
		} else {
			blameNodes = blameLeader
		}
		t.logger.Error().Err(errJoinParty).Msgf("fail to form keygen party with online:%v", onlinePeers)

		return []keygen.Response{{
			Status: common.Fail,
			Blame:  blameNodes,
		}}, nil
	}

	t.tssMetrics.KeygenJoinParty(joinPartyTime, true)
	t.logger.Debug().Msg("keygen party formed")
	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	beforeKeygen := time.Now()

	var responseKeys []keygen.Response
	var blameNode blame.Blame
	var keygenErr error
	for _, algo := range algos {
		instance := keygenInstances[algo]
		var k *crypto.ECPoint
		k, keygenErr = instance.GenerateNewKey(req)
		keygenTime := time.Since(beforeKeygen)
		if keygenErr != nil {
			t.tssMetrics.UpdateKeyGen(keygenTime, false)
			t.logger.Error().Err(keygenErr).Msg("err in keygen")
			blameMgr := instance.GetTssCommonStruct().GetBlameMgr()
			blameNode = *blameMgr.GetBlame()
			break
		} else {
			t.tssMetrics.UpdateKeyGen(keygenTime, true)
		}

		blameNodes := *blameMgr.GetBlame()
		var newPubKey string
		var addr types.AccAddress
		switch algo {
		case common.ECDSA:
			newPubKey, addr, keygenErr = conversion.GetTssPubKeyECDSA(k)
		case common.EdDSA:
			newPubKey, addr, keygenErr = conversion.GetTssPubKeyEDDSA(k)
		default:
			newPubKey, addr, keygenErr = conversion.GetTssPubKeyECDSA(k)
		}
		if keygenErr != nil {
			t.logger.Error().Err(keygenErr).Msg("fail to generate the new Tss key")
			status = common.Fail
			break
		}
		resp := keygen.NewResponse(
			algo,
			newPubKey,
			addr.String(),
			status,
			blameNodes,
		)
		responseKeys = append(responseKeys, resp)
	}

	if keygenErr != nil || status != common.Success {
		return []keygen.Response{{
			Status: common.Fail,
			Blame:  blameNode,
		}}, nil
	}

	return responseKeys, nil
}
