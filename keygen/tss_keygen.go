package keygen

import (
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/crypto"
	bkg "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

type TssKeyGen struct {
	logger          zerolog.Logger
	localNodePubKey string
	preParams       *bkg.LocalPreParams
	tssCommonStruct *common.TssCommon
	stopChan        chan struct{} // channel to indicate whether we should stop
	localParty      *btss.PartyID
	keygenCurrent   *string
	stateManager    storage.LocalStateManager
	commStopChan    chan struct{}
}

func NewTssKeyGen(localP2PID string,
	conf common.TssConfig,
	localNodePubKey string,
	broadcastChan chan *messages.BroadcastMsgChan,
	stopChan chan struct{},
	preParam *bkg.LocalPreParams,
	keygenCurrent *string,
	msgID string,
	stateManager storage.LocalStateManager) *TssKeyGen {
	return &TssKeyGen{
		logger: log.With().
			Str("module", "keygen").
			Str("msgID", msgID).Logger(),
		localNodePubKey: localNodePubKey,
		preParams:       preParam,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID),
		stopChan:        stopChan,
		localParty:      nil,
		keygenCurrent:   keygenCurrent,
		stateManager:    stateManager,
		commStopChan:    make(chan struct{}),
	}
}

func (tKeyGen *TssKeyGen) GetTssKeyGenChannels() chan *p2p.Message {
	return tKeyGen.tssCommonStruct.TssMsg
}

func (tKeyGen *TssKeyGen) GetTssCommonStruct() *common.TssCommon {
	return tKeyGen.tssCommonStruct
}

func (tKeyGen *TssKeyGen) GenerateNewKey(keygenReq Request) (*crypto.ECPoint, error) {
	partiesID, localPartyID, err := common.GetParties(keygenReq.Keys, tKeyGen.localNodePubKey)
	if err != nil {
		return nil, fmt.Errorf("fail to get keygen parties: %w", err)
	}

	keyGenLocalStateItem := storage.KeygenLocalState{
		ParticipantKeys: keygenReq.Keys,
		LocalPartyKey:   tKeyGen.localNodePubKey,
	}

	threshold, err := common.GetThreshold(len(partiesID))
	if err != nil {
		return nil, err
	}
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan bkg.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	if tKeyGen.preParams == nil {
		tKeyGen.logger.Error().Err(err).Msg("error, empty pre-parameters")
		return nil, errors.New("error, empty pre-parameters")
	}
	keyGenParty := bkg.NewLocalParty(params, outCh, endCh, *tKeyGen.preParams)
	partyIDMap := common.SetupPartyIDMap(partiesID)
	err = common.SetupIDMaps(partyIDMap, tKeyGen.tssCommonStruct.PartyIDtoP2PID)
	if err != nil {
		tKeyGen.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}

	tKeyGen.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	tKeyGen.tssCommonStruct.P2PPeers = common.GetPeersID(tKeyGen.tssCommonStruct.PartyIDtoP2PID, tKeyGen.tssCommonStruct.GetLocalPeerID())
	var keyGenWg sync.WaitGroup
	keyGenWg.Add(2)
	// start keygen
	go func() {
		defer keyGenWg.Done()
		defer tKeyGen.logger.Info().Msg("keyGenParty started")
		if err := keyGenParty.Start(); nil != err {
			tKeyGen.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
		}
	}()
	go tKeyGen.tssCommonStruct.ProcessInboundMessages(tKeyGen.commStopChan, &keyGenWg)
	r, err := tKeyGen.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	keyGenWg.Wait()
	return r, err
}

func (tKeyGen *TssKeyGen) processKeyGen(errChan chan struct{},
	outCh <-chan btss.Message,
	endCh <-chan bkg.LocalPartySaveData,
	keyGenLocalStateItem storage.KeygenLocalState) (*crypto.ECPoint, error) {
	defer tKeyGen.logger.Info().Msg("finished keygen process")
	tKeyGen.logger.Info().Msg("start to read messages from local party")
	defer close(tKeyGen.commStopChan)
	tssConf := tKeyGen.tssCommonStruct.GetConf()
	for {
		select {
		case <-errChan: // when keyGenParty return
			tKeyGen.logger.Error().Msg("key gen failed")
			close(tKeyGen.tssCommonStruct.TssMsg)
			return nil, errors.New("error channel closed fail to start local party")

		case <-tKeyGen.stopChan: // when TSS processor receive signal to quit
			close(tKeyGen.tssCommonStruct.TssMsg)
			return nil, errors.New("received exit signal")

		case <-time.After(tssConf.KeyGenTimeout):
			// we bail out after KeyGenTimeoutSeconds
			tKeyGen.logger.Error().Msgf("fail to generate message with %s", tssConf.KeyGenTimeout.String())
			tssCommonStruct := tKeyGen.GetTssCommonStruct()
			localCachedItems := tssCommonStruct.TryGetAllLocalCached()
			blamePeers, err := tssCommonStruct.TssTimeoutBlame(localCachedItems)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to get the blamed peers")
				tssCommonStruct.BlamePeers.SetBlame(common.BlameTssTimeout, nil)
				return nil, fmt.Errorf("fail to get the blamed peers %w", common.ErrTssTimeOut)
			}
			tssCommonStruct.BlamePeers.SetBlame(common.BlameTssTimeout, blamePeers)
			return nil, common.ErrTssTimeOut

		case msg := <-outCh:
			tKeyGen.logger.Debug().Msgf(">>>>>>>>>>msg: %s", msg.String())
			// for the sake of performance, we do not lock the status update
			// we report a rough status of current round
			*tKeyGen.keygenCurrent = msg.Type()
			err := tKeyGen.tssCommonStruct.ProcessOutCh(msg, messages.TSSKeyGenMsg)
			if err != nil {
				return nil, err
			}

		case msg := <-endCh:
			tKeyGen.logger.Debug().Msgf("keygen finished successfully: %s", msg.ECDSAPub.Y().String())
			pubKey, _, err := common.GetTssPubKey(msg.ECDSAPub)
			if err != nil {
				return nil, fmt.Errorf("fail to get thorchain pubkey: %w", err)
			}
			keyGenLocalStateItem.LocalData = msg
			keyGenLocalStateItem.PubKey = pubKey
			if err := tKeyGen.stateManager.SaveLocalState(keyGenLocalStateItem); err != nil {
				return nil, fmt.Errorf("fail to save keygen result to storage: %w", err)
			}

			return msg.ECDSAPub, nil
		}
	}
}
