package keysign

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

type TssKeySign struct {
	logger          zerolog.Logger
	tssCommonStruct *common.TssCommon
	stopChan        *chan struct{} // channel to indicate whether we should stop
	homeBase        string
	syncMsg         chan *p2p.Message
	localParty      *btss.PartyID
	keySignCurrent  *string
}

func NewTssKeySign(localP2PID string,
	conf common.TssConfig,
	broadcastChan chan *p2p.BroadcastMsgChan,
	stopChan *chan struct{},
	keySignCurrent *string,
	msgID string) TssKeySign {
	return TssKeySign{
		logger:          log.With().Str("module", "keySign").Logger(),
		tssCommonStruct: common.NewTssCommon(localP2PID, broadcastChan, conf, msgID),
		stopChan:        stopChan,
		localParty:      nil,
		keySignCurrent:  keySignCurrent,
	}
}

func (tKeySign *TssKeySign) GetTssKeySignChannels() chan *p2p.Message {
	return tKeySign.tssCommonStruct.TssMsg
}

func (tKeySign *TssKeySign) GetTssCommonStruct() *common.TssCommon {
	return tKeySign.tssCommonStruct
}

// signMessage
func (tKeySign *TssKeySign) SignMessage(msgToSign []byte, localStateItem storage.KeygenLocalState, parties []string) (*signing.SignatureData, error) {
	partiesID, localPartyID, err := common.GetParties(parties, localStateItem.LocalPartyKey)
	tKeySign.localParty = localPartyID
	if err != nil {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	if !common.Contains(partiesID, localPartyID) {
		tKeySign.logger.Info().Msgf("we are not in this rounds key sign")
		return nil, nil
	}
	threshold, err := common.GetThreshold(len(localStateItem.ParticipantKeys))
	if err != nil {
		return nil, errors.New("fail to get threshold")
	}

	tKeySign.logger.Debug().Msgf("local party: %+v", localPartyID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan signing.SignatureData, len(partiesID))
	errCh := make(chan struct{})
	m, err := common.MsgToHashInt(msgToSign)
	if err != nil {
		return nil, fmt.Errorf("fail to convert msg to hash int: %w", err)
	}
	keySignParty := signing.NewLocalParty(m, params, localStateItem.LocalData, outCh, endCh)
	partyIDMap := common.SetupPartyIDMap(partiesID)
	err = common.SetupIDMaps(partyIDMap, tKeySign.tssCommonStruct.PartyIDtoP2PID)
	if err != nil {
		tKeySign.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}
	tKeySign.tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keySignParty,
		PartyIDMap: partyIDMap,
	})

	tKeySign.tssCommonStruct.P2PPeers = common.GetPeersID(tKeySign.tssCommonStruct.PartyIDtoP2PID, tKeySign.tssCommonStruct.GetLocalPeerID())

	// start the key sign
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
	if err != nil {
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
			tKeySign.logger.Error().Msgf("fail to sign message with %s", tssConf.KeySignTimeout.String())
			tssCommonStruct := tKeySign.GetTssCommonStruct()
			localCachedItems := tssCommonStruct.TryGetAllLocalCached()
			blamePeers, err := tssCommonStruct.TssTimeoutBlame(localCachedItems)
			if err != nil {
				tKeySign.logger.Error().Err(err).Msg("fail to get the blamed peers")
				tssCommonStruct.BlamePeers.SetBlame(common.BlameTssTimeout, nil)
				return nil, fmt.Errorf("fail to get the blamed peers %w", common.ErrTssTimeOut)
			}
			tssCommonStruct.BlamePeers.SetBlame(common.BlameTssTimeout, blamePeers)
			return nil, common.ErrTssTimeOut
		case msg := <-outCh:
			tKeySign.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			// for the sake of performance, we do not lock the status update
			// we report a rough status of current round
			*tKeySign.keySignCurrent = msg.Type()
			err := tKeySign.tssCommonStruct.ProcessOutCh(msg, p2p.TSSKeySignMsg)
			if err != nil {
				return nil, err
			}
		case m, ok := <-tKeySign.tssCommonStruct.TssMsg:
			if !ok {
				return nil, nil
			}
			var wrappedMsg p2p.WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				tKeySign.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				return nil, err
			}

			// create timeout func so we can ensure TSS doesn't get locked up and frozen
			errChan := make(chan error, 1)
			go func() {
				err := tKeySign.tssCommonStruct.ProcessOneMessage(&wrappedMsg, m.PeerID.String())
				errChan <- err
			}()

			select {
			case err := <-errChan:
				if err != nil {
					tKeySign.logger.Error().Err(err).Msg("fail to process the received message")
					return nil, err
				}
			case <-time.After(tKeySign.tssCommonStruct.GetConf().KeySignTimeout):
				err := errors.New("timeout")
				tKeySign.logger.Error().Err(err).Msg("fail to process the received message")
			}

		case msg := <-endCh:
			tKeySign.logger.Debug().Msg("we have done the key sign")
			return &msg, nil
		}
	}
}

func (tKeySign *TssKeySign) WriteKeySignResult(w http.ResponseWriter, R, S string, status common.Status) {
	// blame := common.NewBlame()
	// blame.SetBlame(tKeySign.tssCommonStruct.Blame.FailReason, tKeySign.tssCommonStruct.Blame.BlameNodes)
	signResp := Response{
		R:      R,
		S:      S,
		Status: status,
		Blame:  tKeySign.tssCommonStruct.BlamePeers,
	}
	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if err != nil {
		tKeySign.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if err != nil {
		tKeySign.logger.Error().Err(err).Msg("fail to write response")
	}
}
