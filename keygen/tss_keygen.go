package keygen

import (
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/binance-chain/tss-lib/crypto"
	bkeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

type TssKeyGen struct {
	logger          zerolog.Logger
	priKey          cryptokey.PrivKey
	preParams       *bkeygen.LocalPreParams
	tssCommonStruct *common.TssCommon
	stopChan        *chan struct{} // channel to indicate whether we should stop
	homeBase        string
	syncMsg         chan *p2p.Message
	localParty      *btss.PartyID
}

func NewTssKeyGen(homeBase, localP2PID string, privKey cryptokey.PrivKey, broadcastchan chan *p2p.BroadcastMsgChan, stopChan *chan struct{}, preParam *bkeygen.LocalPreParams) TssKeyGen {
	return TssKeyGen{
		logger:          log.With().Str("module", "keyGen").Logger(),
		priKey:          privKey,
		preParams:       preParam,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastchan),
		stopChan:        stopChan,
		homeBase:        homeBase,
		syncMsg:         make(chan *p2p.Message),
		localParty:      nil,
	}
}

func (tKeyGen *TssKeyGen) GetTssKeyGenChannels() (chan *p2p.Message, chan *p2p.Message) {
	return tKeyGen.tssCommonStruct.TssMsg, tKeyGen.syncMsg
}

func (tKeyGen *TssKeyGen) GetTssCommonStruct() *common.TssCommon {
	return tKeyGen.tssCommonStruct
}

func (tKeyGen *TssKeyGen) GenerateNewKey(keygenReq KeyGenReq) (*crypto.ECPoint, error) {
	pubKey, err := sdk.Bech32ifyAccPub(tKeyGen.priKey.PubKey())
	if nil != err {
		return nil, fmt.Errorf("fail to genearte the key: %w", err)
	}
	partiesID, localPartyID, err := common.GetParties(keygenReq.Keys, pubKey, true)
	if nil != err {
		return nil, fmt.Errorf("fail to get keygen parties: %w", err)
	}
	keyGenLocalStateItem := common.KeygenLocalStateItem{
		ParticipantKeys: keygenReq.Keys,
		LocalPartyKey:   pubKey,
	}

	threshold, err := common.GetThreshold(len(partiesID))
	if nil != err {
		return nil, err
	}
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan bkeygen.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	keyGenParty := bkeygen.NewLocalParty(params, outCh, endCh, *tKeyGen.preParams)
	partyIDMap := common.SetupPartyIDMap(partiesID)
	err = common.SetupIDMaps(partyIDMap, tKeyGen.tssCommonStruct.PartyIDtoP2PID)
	if nil != err {
		tKeyGen.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}

	tKeyGen.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	tKeyGen.tssCommonStruct.P2PPeers = common.GetPeersID(tKeyGen.tssCommonStruct.PartyIDtoP2PID, tKeyGen.tssCommonStruct.GetLocalPeerID())
	standbyNodes, err := tKeyGen.tssCommonStruct.NodeSync(tKeyGen.syncMsg, p2p.TSSKeyGenSync)
	if err != nil {
		if len(standbyNodes) != len(tKeyGen.tssCommonStruct.P2PPeers) {
			tKeyGen.logger.Debug().Msgf("the nodes online are +%v", standbyNodes)
			return nil, err
			//todo find the nodes to be blamed in node sync
		}
		return nil, err
	}
	// start keygen
	go func() {
		defer tKeyGen.logger.Info().Msg("keyGenParty finished")
		if err := keyGenParty.Start(); nil != err {
			tKeyGen.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
		}
	}()

	r, err := tKeyGen.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	if nil != err {
		tKeyGen.logger.Error().Err(err).Msg("fail to complete keygen")
		tssErr, ok := err.(*btss.Error)
		if ok {
			for _, item := range tssErr.Culprits() {
				tKeyGen.logger.Error().Err(err).Msgf("parties that caused this keygen failure: %s", item.Id)
			}
		}
		for _, item := range keyGenParty.WaitingFor() {
			tKeyGen.logger.Error().Err(err).Msgf("we are still waiting for %s", item.Id)
		}
		return nil, err
	}
	return r, nil
}

func (tKeyGen *TssKeyGen) processKeyGen(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan bkeygen.LocalPartySaveData, keyGenLocalStateItem common.KeygenLocalStateItem) (*crypto.ECPoint, error) {
	defer tKeyGen.logger.Info().Msg("finished keygen process")
	tKeyGen.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when keyGenParty return
			tKeyGen.logger.Error().Msg("key gen failed")
			close(tKeyGen.tssCommonStruct.TssMsg)
			return nil, errors.New("error channel closed fail to start local party")

		case <-*tKeyGen.stopChan: // when TSS processor receive signal to quit
			close(tKeyGen.tssCommonStruct.TssMsg)
			return nil, errors.New("received exit signal")

		case <-time.After(time.Second * common.KeyGenTimeoutSeconds):
			// we bail out after KeyGenTimeoutSeconds
			return nil, fmt.Errorf("fail to finish keyGen with in %d seconds", common.KeyGenTimeoutSeconds)

		case msg := <-outCh:
			tKeyGen.logger.Debug().Msgf(">>>>>>>>>>msg: %s", msg.String())
			err := tKeyGen.tssCommonStruct.ProcessOutCh(msg, p2p.TSSKeyGenMsg)
			if nil != err {
				return nil, err
			}

		case m, ok := <-tKeyGen.tssCommonStruct.TssMsg:
			if !ok {
				return nil, nil
			}
			var wrappedMsg p2p.WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				tKeyGen.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
			}
			err := tKeyGen.tssCommonStruct.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to process the received message")
			}

		case msg := <-endCh:
			tKeyGen.logger.Debug().Msgf("we have done the keygen %s", msg.ECDSAPub.Y().String())
			if err := tKeyGen.AddLocalPartySaveData(tKeyGen.homeBase, msg, keyGenLocalStateItem); nil != err {
				return nil, fmt.Errorf("fail to save key gen result to local store: %w", err)
			}
			return msg.ECDSAPub, nil
		}
	}
}
