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
	keygenCurrent   *string
}

func NewTssKeyGen(homeBase, localP2PID string, conf common.TssConfig, privKey cryptokey.PrivKey, broadcastChan chan *p2p.BroadcastMsgChan, stopChan *chan struct{}, preParam *bkeygen.LocalPreParams, keygenCurrent *string) TssKeyGen {
	return TssKeyGen{
		logger:          log.With().Str("module", "keyGen").Logger(),
		priKey:          privKey,
		preParams:       preParam,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf),
		stopChan:        stopChan,
		homeBase:        homeBase,
		syncMsg:         make(chan *p2p.Message),
		localParty:      nil,
		keygenCurrent:   keygenCurrent,
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
	if tKeyGen.preParams == nil {
		tKeyGen.logger.Error().Err(err).Msg("error, empty pre-parameters")
		return nil, errors.New("error, empty pre-parameters")
	}
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
	standbyPeers, err := tKeyGen.tssCommonStruct.NodeSync(tKeyGen.syncMsg, p2p.TSSKeyGenSync)
	if err != nil {
		tKeyGen.logger.Error().Err(err).Msg("node sync error")
		if err == common.ErrNodeSync {
			tKeyGen.logger.Error().Err(err).Msgf("the nodes online are +%v", standbyPeers)
			_, blamePubKeys, err := tKeyGen.tssCommonStruct.GetBlamePubKeysLists(standbyPeers)
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("error in get blame node pubkey")
				return nil, err
			}
			tKeyGen.tssCommonStruct.BlamePeers.SetBlame(common.BlameNodeSyncCheck, blamePubKeys)
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
	return r, err
}

func (tKeyGen *TssKeyGen) processKeyGen(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan bkeygen.LocalPartySaveData, keyGenLocalStateItem common.KeygenLocalStateItem) (*crypto.ECPoint, error) {
	defer tKeyGen.logger.Info().Msg("finished keygen process")
	tKeyGen.logger.Info().Msg("start to read messages from local party")
	tssConf := tKeyGen.tssCommonStruct.GetConf()
	for {
		select {
		case <-errChan: // when keyGenParty return
			tKeyGen.logger.Error().Msg("key gen failed")
			close(tKeyGen.tssCommonStruct.TssMsg)
			return nil, errors.New("error channel closed fail to start local party")

		case <-*tKeyGen.stopChan: // when TSS processor receive signal to quit
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
				return nil, err
			}
			err := tKeyGen.tssCommonStruct.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
			if err != nil {
				tKeyGen.logger.Error().Err(err).Msg("fail to process the received message")
				return nil, err
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
