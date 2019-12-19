package tss

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"time"

	"github.com/binance-chain/go-sdk/common/types"
	"github.com/binance-chain/tss-lib/crypto"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/btcsuite/btcd/btcec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/secp256k1"
)

func (t *Tss) keygen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key gen request")
	decoder := json.NewDecoder(r.Body)
	var keygenReq KeyGenReq
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if t.getPartyInfo() != nil {
		t.logger.Debug().Msg("another keygen is already in progress")
		resp := KeyGenResp{
			PubKey: "",
			Status: Fail,
		}
		buf, err := json.MarshalIndent(resp, "", "	")
		if nil != err {
			t.logger.Error().Err(err).Msg("fail to marshal response")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write(buf)
		if nil != err {
			t.logger.Error().Err(err).Msg("fail to write to response")
			return
		}
		return
	}

	// we only handle one tss request for a given time
	k, err := t.generateNewKey(keygenReq)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to generate new key")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	newPubKey, addr, err := t.getTssPubKey(k)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to bech32 acc pub key")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if os.Getenv("NET") == "testnet" {
		types.Network = types.TestNetwork
	}
	resp := KeyGenResp{
		PubKey:     newPubKey,
		BNBAddress: addr.String(),
		Status:     Success,
	}
	buf, err := json.Marshal(resp)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	t.logger.Debug().Msg(string(buf))
	_, err = w.Write(buf)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}

func (t *Tss) generateNewKey(keygenReq KeyGenReq) (*crypto.ECPoint, error) {
	t.tssLock.Lock()
	defer t.tssLock.Unlock()
	pubKey, err := sdk.Bech32ifyAccPub(t.priKey.PubKey())
	if nil != err {
		return nil, fmt.Errorf("fail to genearte the key: %w", err)
	}
	partiesID, localPartyID, err := t.getParties(keygenReq.Keys, pubKey, true)
	if nil != err {
		return nil, fmt.Errorf("fail to get keygen parties: %w", err)
	}
	keyGenLocalStateItem := KeygenLocalStateItem{
		ParticipantKeys: keygenReq.Keys,
		LocalPartyKey:   pubKey,
	}

	threshold, err := getThreshold(len(partiesID))
	if nil != err {
		return nil, err
	}
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), threshold)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan keygen.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	keyGenParty := keygen.NewLocalParty(params, outCh, endCh, *t.preParams)

	partyIDMap := setupPartyIDMap(partiesID)
	defer func() {
		t.setPartyInfo(nil)
	}()

	err = setupIDMaps(partyIDMap, t.partyIDtoP2PID)
	if nil != err {
		t.logger.Error().Msgf("error in creating mapping between partyID and P2P ID")
		return nil, err
	}
	// start keygen
	go func() {
		defer t.logger.Info().Msg("keyGenParty finished")
		if err := keyGenParty.Start(); nil != err {
			t.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
			return
		}
		t.setPartyInfo(&PartyInfo{
			Party:      keyGenParty,
			PartyIDMap: partyIDMap,
		})

	}()

	defer t.emptyQueuedMessages(t.keyGenQueuedMsgs)
	r, err := t.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to complete keygen")
		tssErr, ok := err.(*btss.Error)
		if ok {
			for _, item := range tssErr.Culprits() {
				t.logger.Error().Err(err).Msgf("parties that caused this keygen failure: %s", item.Id)
			}
		}
		for _, item := range keyGenParty.WaitingFor() {
			t.logger.Error().Err(err).Msgf("we are still waiting for %s", item.Id)
		}
		return nil, err
	}
	return r, nil
}

func (t *Tss) processKeyGen(errChan chan struct{}, outCh <-chan btss.Message, endCh <-chan keygen.LocalPartySaveData, keyGenLocalStateItem KeygenLocalStateItem) (*crypto.ECPoint, error) {
	defer t.logger.Info().Msg("finished keygen process")
	t.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when keyGenParty return
			t.logger.Error().Msg("key gen failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-t.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(time.Second * KeyGenTimeoutSeconds):
			// we bail out after KeyGenTimeoutSeconds
			return nil, fmt.Errorf("fail to finish keygen with in %d seconds", KeyGenTimeoutSeconds)
		case msg := <-outCh:
			t.logger.Debug().Msgf(">>>>>>>>>>msg: %s", msg.String())
			err := t.processOutCh(msg, TSSKeyGenMsg)
			if nil != err {
				return nil, err
			}
			continue

		case msg := <-endCh:
			t.logger.Debug().Msgf("we have done the keygen %s", msg.ECDSAPub.Y().String())

			if err := t.addLocalPartySaveData(msg, keyGenLocalStateItem); nil != err {
				return nil, fmt.Errorf("fail to save key gen result to local store: %w", err)
			}
			return msg.ECDSAPub, nil
		}
	}
}

func (t *Tss) getTssPubKey(pubKeyPoint *crypto.ECPoint) (string, types.AccAddress, error) {
	tssPubKey := btcec.PublicKey{
		Curve: btcec.S256(),
		X:     pubKeyPoint.X(),
		Y:     pubKeyPoint.Y(),
	}
	var pubKeyCompressed secp256k1.PubKeySecp256k1
	copy(pubKeyCompressed[:], tssPubKey.SerializeCompressed())
	pubKey, err := sdk.Bech32ifyAccPub(pubKeyCompressed)
	addr := types.AccAddress(pubKeyCompressed.Address().Bytes())
	return pubKey, addr, err
}
