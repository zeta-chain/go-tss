package tss

import (
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"path/filepath"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/tendermint/btcd/btcec"
)

// signMessage
func (t *Tss) signMessage(req KeySignReq) (*signing.SignatureData, error) {
	t.tssLock.Lock()
	defer t.tssLock.Unlock()
	if len(req.PoolPubKey) == 0 {
		return nil, fmt.Errorf("empty pool pub key")
	}
	localFileName := fmt.Sprintf("localstate-%d-%s.json", t.port, req.PoolPubKey)
	if len(t.homeBase) > 0 {
		localFileName = filepath.Join(t.homeBase, localFileName)
	}
	storedKeyGenLocalStateItem, err := LoadLocalState(localFileName)
	if nil != err {
		return nil, fmt.Errorf("fail to read local state file: %w", err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if nil != err {
		return nil, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	threshold, err := getThreshold(len(storedKeyGenLocalStateItem.ParticipantKeys))
	if nil != err {
		return nil, err
	}
	t.logger.Debug().Msgf("keysign threshold: %d", threshold)
	partiesID, localPartyID, err := t.getParties(storedKeyGenLocalStateItem.ParticipantKeys, storedKeyGenLocalStateItem.LocalPartyKey, false)
	if nil != err {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	if !contains(partiesID, localPartyID) {
		t.logger.Info().Msgf("we are not in this rounds key sign")
		return nil, nil
	}

	localKeyData, partiesID := ProcessStateFile(storedKeyGenLocalStateItem, partiesID)
	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	t.logger.Debug().Msgf("local party: %+v", localPartyID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan signing.SignatureData, len(partiesID))
	errCh := make(chan struct{})
	m, err := msgToHashInt(msgToSign)
	if nil != err {
		return nil, fmt.Errorf("fail to convert msg to hash int: %w", err)
	}
	keySignParty := signing.NewLocalParty(m, params, localKeyData, outCh, endCh)
	partyIDMap := setupPartyIDMap(partiesID)

	defer func() {
		t.setPartyInfo(nil)
	}()

	err = setupIDMaps(partyIDMap, t.partyIDtoP2PID)
	if nil != err {
		t.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	} //start key sign
	go func() {
		if err := keySignParty.Start(); nil != err {
			t.logger.Error().Err(err).Msg("fail to start key sign party")
			close(errCh)
		}
		t.setPartyInfo(&PartyInfo{
			Party:      keySignParty,
			PartyIDMap: partyIDMap,
		})
		t.logger.Debug().Msg("local party is ready")
	}()

	defer t.emptyQueuedMessages(t.keySignQueuedMsgs)
	result, err := t.processKeySign(errCh, outCh, endCh)
	if nil != err {
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}
	t.logger.Info().Msg("successfully sign the message")
	return result, nil
}
func (t *Tss) processKeySign(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan signing.SignatureData) (*signing.SignatureData, error) {
	defer t.logger.Info().Msg("key sign finished")
	t.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when key sign return
			t.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-t.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(time.Second * KeySignTimeoutSeconds):
			// we bail out after KeySignTimeoutSeconds
			return nil, fmt.Errorf("fail to sign message with in %d seconds", KeySignTimeoutSeconds)
		case msg := <-outCh:
			t.logger.Debug().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			err := t.processOutCh(msg, TSSKeySignMsg)
			if nil != err {
				return nil, err
			}
			continue
		case msg := <-endCh:
			t.logger.Debug().Msg("we have done the key sign")
			return &msg, nil
		}
	}
}

// keysign process keysign request
func (t *Tss) keysign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key sign request")
	decoder := json.NewDecoder(r.Body)
	var keySignReq KeySignReq
	if err := decoder.Decode(&keySignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	if t.getPartyInfo() != nil {
		t.logger.Error().Msg("another tss key sign is in progress")
		t.writeKeySignResult(w, "", "", Fail)
		return
	}
	signatureData, err := t.signMessage(keySignReq)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to sign message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if nil == signatureData {
		t.writeKeySignResult(w, "", "", NA)
	} else {
		t.writeKeySignResult(w, base64.StdEncoding.EncodeToString(signatureData.R), base64.StdEncoding.EncodeToString(signatureData.S), Success)
	}
}

func (t *Tss) writeKeySignResult(w http.ResponseWriter, R, S string, status Status) {
	signResp := KeySignResp{
		R:      R,
		S:      S,
		Status: Success,
	}
	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write response")
	}
}

func msgToHashInt(msg []byte) (*big.Int, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if nil != err {
		return nil, fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hashToInt(h.Sum(nil), btcec.S256()), nil
}

func hashToInt(hash []byte, c elliptic.Curve) *big.Int {
	orderBits := c.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}
