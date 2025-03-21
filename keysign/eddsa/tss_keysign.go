package eddsa

import (
	"encoding/json"
	"math/big"
	"sort"
	"strconv"
	"sync"
	"time"

	tsslibcommon "github.com/bnb-chain/tss-lib/common"
	eddsakeygen "github.com/bnb-chain/tss-lib/eddsa/keygen"
	"github.com/bnb-chain/tss-lib/eddsa/signing"
	btss "github.com/bnb-chain/tss-lib/tss"
	tcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"go.uber.org/atomic"

	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/go-tss/storage"
)

type KeySign struct {
	logger          zerolog.Logger
	tssCommonStruct *common.TssCommon
	stopChan        chan struct{} // channel to indicate whether we should stop
	localParty      *btss.PartyID
	commStopChan    chan struct{}
	p2pComm         *p2p.Communication
	stateManager    storage.LocalStateManager
}

func New(
	localP2PID string,
	conf common.TssConfig,
	broadcastChan chan *messages.BroadcastMsgChan,
	stopChan chan struct{},
	msgID string,
	privKey tcrypto.PrivKey,
	p2pComm *p2p.Communication,
	stateManager storage.LocalStateManager,
	msgNum int,
	logger zerolog.Logger,
) *KeySign {
	logger = logger.With().Str(logs.Component, "keygen").Str(logs.MsgID, msgID).Logger()

	return &KeySign{
		logger:          logger,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID, privKey, msgNum, logger),
		stopChan:        stopChan,
		localParty:      nil,
		commStopChan:    make(chan struct{}),
		p2pComm:         p2pComm,
		stateManager:    stateManager,
	}
}

func (tKeySign *KeySign) GetTssKeySignChannels() chan *p2p.Message {
	return tKeySign.tssCommonStruct.TssMsg
}

func (tKeySign *KeySign) GetTssCommonStruct() *common.TssCommon {
	return tKeySign.tssCommonStruct
}

func (tKeySign *KeySign) startBatchSigning(keySignPartyMap *sync.Map, msgNum int) bool {
	// start the batch sign
	var keySignWg sync.WaitGroup
	ret := atomic.NewBool(true)
	keySignWg.Add(msgNum)
	keySignPartyMap.Range(func(_, value any) bool {
		eachParty := value.(btss.Party)

		go func(eachParty btss.Party) {
			defer keySignWg.Done()

			if err := eachParty.Start(); err != nil {
				tKeySign.logger.Error().Err(err).
					Fields(logs.Party(eachParty)).
					Msg("Failed to start key sign party")

				ret.Store(false)
				return
			}

			tKeySign.logger.Info().Fields(logs.Party(eachParty)).Msg("Local party is ready")
		}(eachParty)

		return true
	})
	keySignWg.Wait()
	return ret.Load()
}

// signMessage
func (tKeySign *KeySign) SignMessage(
	msgsToSign [][]byte,
	localStateItem storage.KeygenLocalState,
	parties []string,
) ([]*tsslibcommon.SignatureData, error) {
	partiesID, localPartyID, err := conversion.GetParties(parties, localStateItem.LocalPartyKey)
	tKeySign.localParty = localPartyID
	if err != nil {
		return nil, errors.Wrap(err, "fail to form key sign party")
	}

	if !common.Contains(partiesID, localPartyID) {
		tKeySign.logger.Info().Msg("we are not in this rounds key sign")
		return nil, nil
	}
	threshold, err := conversion.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		return nil, errors.Wrap(err, "fail to get threshold")
	}

	outCh := make(chan btss.Message, 2*len(partiesID)*len(msgsToSign))
	endCh := make(chan tsslibcommon.SignatureData, len(partiesID)*len(msgsToSign))
	errCh := make(chan struct{})

	keySignPartyMap := new(sync.Map)

	for i, val := range msgsToSign {
		m, err := common.MsgToHashInt(val, common.EdDSA)
		if err != nil {
			return nil, errors.Wrap(err, "fail to convert msg to hash int")
		}

		moniker := m.String() + ":" + strconv.Itoa(i)
		partiesID, eachLocalPartyID, err := conversion.GetParties(parties, localStateItem.LocalPartyKey)
		ctx := btss.NewPeerContext(partiesID)
		if err != nil {
			return nil, errors.Wrap(err, "error to create parties in batch signing")
		}

		eachLocalPartyID.Moniker = moniker
		params := btss.NewParameters(btss.Edwards(), ctx, eachLocalPartyID, len(partiesID), threshold)
		var localData eddsakeygen.LocalPartySaveData

		err = json.Unmarshal(localStateItem.LocalData, &localData)
		if err != nil {
			return nil, errors.Wrap(err, "fail to unmarshal LocalPartySaveData")
		}

		keySignParty := signing.NewLocalParty(m, params, localData, outCh, endCh)
		keySignPartyMap.Store(moniker, keySignParty)
	}

	tKeySign.logger.Debug().Msgf("local party: %+v", localPartyID)

	blameMgr := tKeySign.tssCommonStruct.GetBlameMgr()
	partyIDMap := conversion.SetupPartyIDMap(partiesID)
	err1 := conversion.SetupIDMaps(partyIDMap, tKeySign.tssCommonStruct.PartyIDtoP2PID)
	err2 := conversion.SetupIDMaps(partyIDMap, blameMgr.PartyIDtoP2PID)
	if err1 != nil || err2 != nil {
		tKeySign.logger.Error().Err(err).Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}

	tKeySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		PartyMap:   keySignPartyMap,
		PartyIDMap: partyIDMap,
	})

	blameMgr.SetPartyInfo(keySignPartyMap, partyIDMap)
	tKeySign.tssCommonStruct.P2PPeersLock.Lock()
	tKeySign.tssCommonStruct.P2PPeers = conversion.GetPeersID(
		tKeySign.tssCommonStruct.PartyIDtoP2PID,
		tKeySign.tssCommonStruct.GetLocalPeerID(),
	)
	tKeySign.tssCommonStruct.P2PPeersLock.Unlock()
	var keySignWg sync.WaitGroup
	keySignWg.Add(2)
	// start the key sign
	go func() {
		defer keySignWg.Done()
		ret := tKeySign.startBatchSigning(keySignPartyMap, len(msgsToSign))
		if !ret {
			tKeySign.logger.Error().Err(err).Msg("fail to start key sign party")
			close(errCh)
		}
		tKeySign.logger.Debug().Msg("local party is ready")
	}()
	go tKeySign.tssCommonStruct.ProcessInboundMessages(tKeySign.commStopChan, &keySignWg)
	results, err := tKeySign.processKeySign(len(msgsToSign), errCh, outCh, endCh)
	if err != nil {
		close(tKeySign.commStopChan)
		return nil, errors.Wrap(err, "fail to process key sign")
	}

	select {
	case <-time.After(time.Second * 5):
		close(tKeySign.commStopChan)
	case <-tKeySign.tssCommonStruct.GetTaskDone():
		close(tKeySign.commStopChan)
	}
	keySignWg.Wait()

	tKeySign.logger.Info().
		Stringer(logs.Host, tKeySign.p2pComm.GetHost().ID()).
		Msg("Successfully signed the message")

	sort.SliceStable(results, func(i, j int) bool {
		a := new(big.Int).SetBytes(results[i].M)
		b := new(big.Int).SetBytes(results[j].M)

		return a.Cmp(b) >= 0
	})

	return results, nil
}

func (tKeySign *KeySign) processKeySign(
	reqNum int,
	errChan chan struct{},
	outCh <-chan btss.Message,
	endCh <-chan tsslibcommon.SignatureData,
) ([]*tsslibcommon.SignatureData, error) {
	defer tKeySign.logger.Debug().Msg("key sign finished")
	tKeySign.logger.Debug().Msg("start to read messages from local party")
	var signatures []*tsslibcommon.SignatureData
	tssConf := tKeySign.tssCommonStruct.GetConf()
	blameMgr := tKeySign.tssCommonStruct.GetBlameMgr()
	for {
		select {
		case <-errChan: // when key sign return
			tKeySign.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-tKeySign.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(tssConf.KeySignTimeout):
			// we bail out after KeySignTimeoutSeconds
			tKeySign.logger.Error().
				Float64("timeout", tssConf.KeySignTimeout.Seconds()).
				Msg("Failed to sign message due to timeout")

			lastMsg := blameMgr.GetLastMsg()
			failReason := blameMgr.GetBlame().FailReason
			if failReason == "" {
				failReason = blame.TssTimeout
			}

			tKeySign.tssCommonStruct.P2PPeersLock.RLock()
			threshold, err := conversion.GetThreshold(len(tKeySign.tssCommonStruct.P2PPeers) + 1)
			tKeySign.tssCommonStruct.P2PPeersLock.RUnlock()
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("error in get the threshold for generate blame")
			}
			if !lastMsg.IsBroadcast() {
				blameNodesUnicast, err := blameMgr.GetUnicastBlame(lastMsg.Type())
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("error in get unicast blame")
				}
				if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
					blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, lastMsg.IsBroadcast(), lastMsg.Type())
				}
			} else {
				blameNodesUnicast, err := blameMgr.GetUnicastBlame(conversion.GetPreviousKeySignUicast(lastMsg.Type()))
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("error in get unicast blame")
				}
				if len(blameNodesUnicast) > 0 && len(blameNodesUnicast) <= threshold {
					blameMgr.GetBlame().SetBlame(failReason, blameNodesUnicast, lastMsg.IsBroadcast(), lastMsg.Type())
				}
			}

			blameNodesBroadcast, err := blameMgr.GetBroadcastBlame(lastMsg.Type())
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("error in get broadcast blame")
			}
			blameMgr.GetBlame().AddBlameNodes(blameNodesBroadcast...)

			// if we cannot find the blame node, we check whether everyone send me the share
			if len(blameMgr.GetBlame().BlameNodes) == 0 {
				blameNodesMisingShare, isUnicast, err := blameMgr.TssMissingShareBlame(
					messages.EDDSAKEYSIGNROUNDS,
					messages.EDDSAKEYSIGN,
				)
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to get the node of missing share ")
				}

				if len(blameNodesMisingShare) > 0 && len(blameNodesMisingShare) <= threshold {
					blameMgr.GetBlame().AddBlameNodes(blameNodesMisingShare...)
					blameMgr.GetBlame().IsUnicast = isUnicast
				}
			}

			return nil, blame.ErrTimeoutTSS
		case msg := <-outCh:
			tKeySign.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			tKeySign.tssCommonStruct.GetBlameMgr().SetLastMsg(msg)
			err := tKeySign.tssCommonStruct.ProcessOutCh(msg, messages.TSSKeySignMsg)
			if err != nil {
				return nil, err
			}

		//nolint
		case msg := <-endCh:
			signatures = append(signatures, &msg)
			if len(signatures) == reqNum {
				tKeySign.logger.Debug().Msg("we have done the key sign")
				err := tKeySign.tssCommonStruct.NotifyTaskDone()
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to broadcast the keysign done")
				}
				//export the address book
				address := tKeySign.p2pComm.ExportPeerAddress()
				if err := tKeySign.stateManager.SaveAddressBook(address); err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to save the peer addresses")
				}
				return signatures, nil
			}
		}
	}
}
