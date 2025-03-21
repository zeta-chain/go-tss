package eddsa

import (
	"encoding/json"
	"sync"
	"time"

	bcrypto "github.com/bnb-chain/tss-lib/crypto"
	eddsakg "github.com/bnb-chain/tss-lib/eddsa/keygen"
	btss "github.com/bnb-chain/tss-lib/tss"
	tcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/rs/zerolog"
	"gitlab.com/tozd/go/errors"

	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/go-tss/storage"
)

type KeyGen struct {
	logger          zerolog.Logger
	localNodePubKey string
	tssCommonStruct *common.TssCommon
	stopChan        chan struct{} // channel to indicate whether we should stop
	localParty      *btss.PartyID
	stateManager    storage.LocalStateManager
	commStopChan    chan struct{}
	p2pComm         *p2p.Communication
}

func New(
	localP2PID string,
	conf common.TssConfig,
	localNodePubKey string,
	broadcastChan chan *messages.BroadcastMsgChan,
	stopChan chan struct{},
	msgID string,
	stateManager storage.LocalStateManager,
	privateKey tcrypto.PrivKey,
	p2pComm *p2p.Communication,
	logger zerolog.Logger,
) *KeyGen {
	logger = logger.With().Str(logs.Component, "keygen").Str(logs.MsgID, msgID).Logger()

	return &KeyGen{
		logger:          logger,
		localNodePubKey: localNodePubKey,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID, privateKey, 1, logger),
		stopChan:        stopChan,
		localParty:      nil,
		stateManager:    stateManager,
		commStopChan:    make(chan struct{}),
		p2pComm:         p2pComm,
	}
}

func (tKeyGen *KeyGen) GetTssKeyGenChannels() chan *p2p.Message {
	return tKeyGen.tssCommonStruct.TssMsg
}

func (tKeyGen *KeyGen) GetTssCommonStruct() *common.TssCommon {
	return tKeyGen.tssCommonStruct
}

func (tKeyGen *KeyGen) GenerateNewKey(req keygen.Request) (*bcrypto.ECPoint, error) {
	keyGenPartyMap := new(sync.Map)
	partiesID, localPartyID, err := conversion.GetParties(req.Keys, tKeyGen.localNodePubKey)
	if err != nil {
		return nil, errors.Wrap(err, "failed to get keygen parties")
	}

	keyGenLocalStateItem := storage.KeygenLocalState{
		ParticipantKeys: req.Keys,
		LocalPartyKey:   tKeyGen.localNodePubKey,
	}

	threshold, err := conversion.GetThreshold(len(partiesID))
	if err != nil {
		return nil, errors.Wrap(err, "failed to get threshold")
	}

	var (
		ctx         = btss.NewPeerContext(partiesID)
		params      = btss.NewParameters(btss.Edwards(), ctx, localPartyID, len(partiesID), threshold)
		outCh       = make(chan btss.Message, len(partiesID))
		endCh       = make(chan eddsakg.LocalPartySaveData, len(partiesID))
		errChan     = make(chan struct{})
		blameMgr    = tKeyGen.tssCommonStruct.GetBlameMgr()
		keyGenParty = eddsakg.NewLocalParty(params, outCh, endCh)
		partyIDMap  = conversion.SetupPartyIDMap(partiesID)
	)

	err = conversion.SetupIDMaps(partyIDMap, tKeyGen.tssCommonStruct.PartyIDtoP2PID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup ID maps #1")
	}

	err = conversion.SetupIDMaps(partyIDMap, blameMgr.PartyIDtoP2PID)
	if err != nil {
		return nil, errors.Wrap(err, "failed to setup ID maps #2")
	}

	keyGenPartyMap.Store("", keyGenParty)
	partyInfo := &common.PartyInfo{
		PartyMap:   keyGenPartyMap,
		PartyIDMap: partyIDMap,
	}

	tKeyGen.tssCommonStruct.SetPartyInfo(partyInfo)
	blameMgr.SetPartyInfo(keyGenPartyMap, partyIDMap)
	tKeyGen.tssCommonStruct.P2PPeersLock.Lock()
	tKeyGen.tssCommonStruct.P2PPeers = conversion.GetPeersID(
		tKeyGen.tssCommonStruct.PartyIDtoP2PID,
		tKeyGen.tssCommonStruct.GetLocalPeerID(),
	)
	tKeyGen.tssCommonStruct.P2PPeersLock.Unlock()

	var keyGenWg sync.WaitGroup
	keyGenWg.Add(2)

	// start keygen
	go func() {
		defer keyGenWg.Done()
		defer tKeyGen.logger.Debug().Msg("keyGenParty started")
		if err := keyGenParty.Start(); nil != err {
			tKeyGen.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
		}
	}()

	go tKeyGen.tssCommonStruct.ProcessInboundMessages(tKeyGen.commStopChan, &keyGenWg)

	r, err := tKeyGen.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	if err != nil {
		close(tKeyGen.commStopChan)
		return nil, errors.Wrap(err, "failed to process key gen")
	}

	select {
	case <-time.After(time.Second * 5):
		close(tKeyGen.commStopChan)

	case <-tKeyGen.tssCommonStruct.GetTaskDone():
		close(tKeyGen.commStopChan)
	}

	keyGenWg.Wait()

	return r, err
}

func (tKeyGen *KeyGen) processKeyGen(errChan chan struct{},
	outCh <-chan btss.Message,
	endCh <-chan eddsakg.LocalPartySaveData,
	keyGenLocalStateItem storage.KeygenLocalState) (*bcrypto.ECPoint, error) {
	defer tKeyGen.logger.Debug().Msg("finished keygen process")
	tKeyGen.logger.Debug().Msg("start to read messages from local party")
	tssConf := tKeyGen.tssCommonStruct.GetConf()
	blameMgr := tKeyGen.tssCommonStruct.GetBlameMgr()
	for {
		select {
		case <-errChan: // when keyGenParty return
			tKeyGen.logger.Error().Msg("key gen failed")
			return nil, errors.New("error channel closed fail to start local party")

		case <-tKeyGen.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")

		case <-time.After(tssConf.KeyGenTimeout):
			// we bail out after KeyGenTimeoutSeconds
			tKeyGen.logger.Error().Msgf("fail to generate message with %s", tssConf.KeyGenTimeout.String())
			lastMsg := blameMgr.GetLastMsg()
			failReason := blameMgr.GetBlame().FailReason
			if failReason == "" {
				failReason = blame.TssTimeout
			}
			if lastMsg == nil {
				tKeyGen.logger.Error().Msg("fail to start the keygen, the last produced message of this node is none")
				return nil, errors.New("timeout before shared message is generated")
			}
			blameNodesUnicast, err := blameMgr.GetUnicastBlame(messages.KEYGEN2aUnicast)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get unicast blame")
			}
			threshold, err := conversion.GetThreshold(len(tKeyGen.tssCommonStruct.P2PPeers) + 1)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get the threshold to generate blame")
			}

			if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
				blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, lastMsg.IsBroadcast(), lastMsg.Type())
			}
			blameNodesBroadcast, err := blameMgr.GetBroadcastBlame(lastMsg.Type())
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get broadcast blame")
			}
			blameMgr.GetBlame().AddBlameNodes(blameNodesBroadcast...)

			// if we cannot find the blame node, we check whether everyone send me the share
			if len(blameMgr.GetBlame().BlameNodes) == 0 {
				blameNodesMisingShare, isUnicast, err := blameMgr.TssMissingShareBlame(
					messages.EDDSAKEYGENROUNDS,
					messages.EDDSAKEYGEN,
				)
				if err != nil {
					tKeyGen.logger.Error().Err(err).Msg("fail to get the node of missing share ")
				}
				if len(blameNodesMisingShare) > 0 && len(blameNodesMisingShare) <= threshold {
					blameMgr.GetBlame().AddBlameNodes(blameNodesMisingShare...)
					blameMgr.GetBlame().IsUnicast = isUnicast
				}
			}
			return nil, blame.ErrTimeoutTSS

		case msg := <-outCh:
			tKeyGen.logger.Debug().Msgf(">>>>>>>>>>msg: %s", msg.String())
			blameMgr.SetLastMsg(msg)
			err := tKeyGen.tssCommonStruct.ProcessOutCh(msg, messages.TSSKeyGenMsg)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to process the message")
				return nil, err
			}

		case msg := <-endCh:
			tKeyGen.logger.Debug().Msgf("keygen finished successfully: %s", msg.EDDSAPub.Y().String())
			err := tKeyGen.tssCommonStruct.NotifyTaskDone()
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to broadcast the keysign done")
			}
			pubKey, _, err := conversion.GetTssPubKeyEDDSA(msg.EDDSAPub)
			if err != nil {
				return nil, errors.Wrap(err, "failed to get thorchain pubkey")
			}
			marshaledMsg, err := json.Marshal(msg)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to marshal the result")
				return nil, errors.New("fail to marshal the result")
			}
			keyGenLocalStateItem.LocalData = marshaledMsg
			keyGenLocalStateItem.PubKey = pubKey
			if err := tKeyGen.stateManager.SaveLocalState(keyGenLocalStateItem); err != nil {
				return nil, errors.Wrap(err, "failed to save keygen result to storage")
			}
			address := tKeyGen.p2pComm.ExportPeerAddress()
			if err := tKeyGen.stateManager.SaveAddressBook(address); err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to save the peer addresses")
			}
			return msg.EDDSAPub, nil
		}
	}
}
