package keysign

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"path/filepath"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

type TssKeySign struct {
	logger          zerolog.Logger
	priKey          cryptokey.PrivKey
	tssCommonStruct *common.TssCommon
	stopChan        *chan struct{} // channel to indicate whether we should stop
	homeBase        string
	syncMsg         chan *p2p.Message
	localParty      *btss.PartyID
}

func NewTssKeySign(homeBase, localP2PID string, conf common.TssConfig, privKey cryptokey.PrivKey, broadcastChan chan *p2p.BroadcastMsgChan, stopChan *chan struct{}) TssKeySign {
	return TssKeySign{
		logger:          log.With().Str("module", "keySign").Logger(),
		priKey:          privKey,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf),
		stopChan:        stopChan,
		homeBase:        homeBase,
		syncMsg:         make(chan *p2p.Message),
		localParty:      nil,
	}
}

func (tKeySign *TssKeySign) GetTssKeySignChannels() (chan *p2p.Message, chan *p2p.Message) {
	return tKeySign.tssCommonStruct.TssMsg, tKeySign.syncMsg
}

func (tKeySign *TssKeySign) GetTssCommonStruct() *common.TssCommon {
	return tKeySign.tssCommonStruct
}

// signMessage
func (tKeySign *TssKeySign) SignMessage(req KeySignReq) (*signing.SignatureData, error) {
	if len(req.PoolPubKey) == 0 {
		return nil, fmt.Errorf("empty pool pub key")
	}
	localFileName := fmt.Sprintf("localstate-%s.json", req.PoolPubKey)
	if len(tKeySign.homeBase) > 0 {
		localFileName = filepath.Join(tKeySign.homeBase, localFileName)
	}
	storedKeyGenLocalStateItem, err := common.LoadLocalState(localFileName)
	if nil != err {
		return nil, fmt.Errorf("fail to read local state file: %w", err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if nil != err {
		return nil, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	threshold, err := common.GetThreshold(len(storedKeyGenLocalStateItem.ParticipantKeys))
	if nil != err {
		return nil, err
	}
	tKeySign.logger.Debug().Msgf("keysign threshold: %d", threshold)
	partiesID, localPartyID, err := common.GetParties(storedKeyGenLocalStateItem.ParticipantKeys, storedKeyGenLocalStateItem.LocalPartyKey, false)
	if nil != err {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	if !common.Contains(partiesID, localPartyID) {
		tKeySign.logger.Info().Msgf("we are not in this rounds key sign")
		return nil, nil
	}

	localKeyData, partiesID := common.ProcessStateFile(storedKeyGenLocalStateItem, partiesID)
	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	tKeySign.logger.Debug().Msgf("local party: %+v", localPartyID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan signing.SignatureData, len(partiesID))
	errCh := make(chan struct{})
	m, err := common.MsgToHashInt(msgToSign)
	if nil != err {
		return nil, fmt.Errorf("fail to convert msg to hash int: %w", err)
	}
	keySignParty := signing.NewLocalParty(m, params, localKeyData, outCh, endCh)
	partyIDMap := common.SetupPartyIDMap(partiesID)
	err = common.SetupIDMaps(partyIDMap, tKeySign.tssCommonStruct.PartyIDtoP2PID)
	if nil != err {
		tKeySign.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}
	tKeySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keySignParty,
		PartyIDMap: partyIDMap,
	})

	tKeySign.tssCommonStruct.P2PPeers = common.GetPeersID(tKeySign.tssCommonStruct.PartyIDtoP2PID, tKeySign.tssCommonStruct.GetLocalPeerID())
	standbyNodes, err := tKeySign.tssCommonStruct.NodeSync(tKeySign.syncMsg, p2p.TSSKeySignSync)
	if err != nil {
		tKeySign.logger.Error().Err(err).Msg("node sync error")
		if len(standbyNodes) != len(tKeySign.tssCommonStruct.P2PPeers) {
			tKeySign.logger.Debug().Msgf("the nodes online are +%v", standbyNodes)
			return nil, err
			//todo find the nodes to be blamed in node sync
		}
		return nil, err
	}

	//start the key sign
	go func() {
		if err := keySignParty.Start(); nil != err {
			tKeySign.logger.Error().Err(err).Msg("fail to start key sign party")
			close(errCh)
		}
		tKeySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
			Party:      keySignParty,
			PartyIDMap: partyIDMap,
		})
		tKeySign.logger.Debug().Msg("local party is ready")
	}()

	result, err := tKeySign.processKeySign(errCh, outCh, endCh)
	if nil != err {
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}
	tKeySign.logger.Info().Msg("successfully sign the message")
	return result, nil
}
func (tKeySign *TssKeySign) processKeySign(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan signing.SignatureData) (*signing.SignatureData, error) {
	defer tKeySign.logger.Info().Msg("key sign finished")
	tKeySign.logger.Info().Msg("start to read messages from local party")
	tssConf := tKeySign.tssCommonStruct.GetConf()
	for {
		select {
		case <-errChan: // when key sign return
			tKeySign.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-*tKeySign.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(tssConf.KeySignTimeout):
			// we bail out after KeySignTimeoutSeconds
			return nil, fmt.Errorf("fail to sign message with in %d seconds", tssConf.KeySignTimeout)
		case msg := <-outCh:
			tKeySign.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			err := tKeySign.tssCommonStruct.ProcessOutCh(msg, p2p.TSSKeySignMsg)
			if nil != err {
				return nil, err
			}
		case m, ok := <-tKeySign.tssCommonStruct.TssMsg:
			if !ok {
				return nil, nil
			}
			var wrappedMsg p2p.WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				tKeySign.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}
			err := tKeySign.tssCommonStruct.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("failed to process the received message")
			}
		case msg := <-endCh:
			tKeySign.logger.Debug().Msg("we have done the key sign")
			return &msg, nil
		}
	}
}

func (tKeySign *TssKeySign) WriteKeySignResult(w http.ResponseWriter, R, S string, status common.Status) {
	signResp := KeySignResp{
		R:      R,
		S:      S,
		Status: status,
	}
	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if nil != err {
		tKeySign.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if nil != err {
		tKeySign.logger.Error().Err(err).Msg("fail to write response")
	}
}
