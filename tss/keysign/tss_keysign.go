package keysign

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	bkeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/rs/zerolog"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/tss/common"
	"net/http"
	"path/filepath"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog/log"
)

type TssKeySign struct {
	logger          zerolog.Logger
	priKey          cryptokey.PrivKey
	preParams       *bkeygen.LocalPreParams
	tssCommonStruct common.TssCommon
	stopChan        *chan struct{} // channel to indicate whether we should stop
	homeBase        string
	syncMsg         chan *p2p.Message
	localParty      *btss.PartyID
}

func NewTssKeySign(homeBase, localP2PID string, privKey cryptokey.PrivKey, broadcastChan *chan *p2p.BroadcastMsgChan, stopChan *chan struct{}, preParam *bkeygen.LocalPreParams) *TssKeySign {
	return &TssKeySign{
		logger:          log.With().Str("module", "keySign").Logger(),
		priKey:          privKey,
		preParams:       preParam,
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan),
		stopChan:        stopChan,
		homeBase:        homeBase,
		syncMsg:         make(chan *p2p.Message),
		localParty:      nil,
	}
}

func (keySign *TssKeySign) GetMyTssMsgChannels() (*chan *p2p.Message, *chan *p2p.Message) {
	return &keySign.tssCommonStruct.TssMsg, &keySign.syncMsg
}

func (keySign *TssKeySign) GetTssCommonStruct() *common.TssCommon {
	return &keySign.tssCommonStruct
}

// signMessage
func (keySign *TssKeySign) SignMessage(req KeySignReq) (*signing.SignatureData, error) {
	if len(req.PoolPubKey) == 0 {
		return nil, fmt.Errorf("empty pool pub key")
	}
	localFileName := fmt.Sprintf("localstate-%s.json", req.PoolPubKey)
	if len(keySign.homeBase) > 0 {
		localFileName = filepath.Join(keySign.homeBase, localFileName)
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
	keySign.logger.Debug().Msgf("keysign threshold: %d", threshold)
	partiesID, localPartyID, err := common.GetParties(storedKeyGenLocalStateItem.ParticipantKeys, storedKeyGenLocalStateItem.LocalPartyKey, false)
	if nil != err {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	if !common.Contains(partiesID, localPartyID) {
		keySign.logger.Info().Msgf("we are not in this rounds key sign")
		return nil, nil
	}

	localKeyData, partiesID := common.ProcessStateFile(storedKeyGenLocalStateItem, partiesID)
	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	keySign.logger.Debug().Msgf("local party: %+v", localPartyID)
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
	err = common.SetupIDMaps(partyIDMap, keySign.tssCommonStruct.PartyIDtoP2PID)
	if nil != err {
		keySign.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}
	keySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keySignParty,
		PartyIDMap: partyIDMap,
	})

	keySign.tssCommonStruct.P2PPeers = common.GetPeersID(keySign.tssCommonStruct.PartyIDtoP2PID, keySign.tssCommonStruct.GetLocalPeerID())
	standbyNodes, err := keySign.tssCommonStruct.NodeSync(keySign.syncMsg, p2p.TSSKeySignSync)
	if err != nil {
		if len(standbyNodes) != 4 {
			keySign.logger.Debug().Msgf("the nodes online are +%v", standbyNodes)
			//todo find the nodes to be blamed in node sync
		}
		return nil, err
	}

	//start the key sign
	go func() {
		if err := keySignParty.Start(); nil != err {
			keySign.logger.Error().Err(err).Msg("fail to start key sign party")
			close(errCh)
		}
		keySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
			Party:      keySignParty,
			PartyIDMap: partyIDMap,
		})
		keySign.logger.Debug().Msg("local party is ready")
	}()

	result, err := keySign.processKeySign(errCh, outCh, endCh)
	if nil != err {
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}
	keySign.logger.Info().Msg("successfully sign the message")
	return result, nil
}
func (keySign *TssKeySign) processKeySign(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan signing.SignatureData) (*signing.SignatureData, error) {
	defer keySign.logger.Info().Msg("key sign finished")
	keySign.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when key sign return
			keySign.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-*keySign.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(time.Second * common.KeySignTimeoutSeconds):
			// we bail out after KeySignTimeoutSeconds
			return nil, fmt.Errorf("fail to sign message with in %d seconds", common.KeySignTimeoutSeconds)
		case msg := <-outCh:
			keySign.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			err := keySign.tssCommonStruct.ProcessOutCh(msg, p2p.TSSKeySignMsg)
			if nil != err {
				return nil, err
			}
			continue

		case m, ok := <-keySign.tssCommonStruct.TssMsg:
			if !ok {
				return nil, nil
			}
			var wrappedMsg p2p.WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				keySign.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}
			err := keySign.tssCommonStruct.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
			if err != nil {
				fmt.Println(err)
			}
			continue
		case msg := <-endCh:
			keySign.logger.Debug().Msg("we have done the key sign")
			return &msg, nil
		}
	}
}

func (keySign *TssKeySign) WriteKeySignResult(w http.ResponseWriter, R, S string, status common.Status) {
	signResp := KeySignResp{
		R:      R,
		S:      S,
		Status: status,
	}
	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if nil != err {
		keySign.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if nil != err {
		keySign.logger.Error().Err(err).Msg("fail to write response")
	}
}
