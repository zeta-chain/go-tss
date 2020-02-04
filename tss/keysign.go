package tss

import (
	"encoding/base64"
	"encoding/json"
	"net/http"
	"sync/atomic"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) KeySign(w http.ResponseWriter, r *http.Request) {
	t.tssKeySignLocker.Lock()
	defer t.tssKeySignLocker.Unlock()
	var keySignReq keysign.KeySignReq
	keySignFlag := common.Success
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
	if err := decoder.Decode(&keySignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msgID, err := t.requestToMsgId(keySignReq)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	keysignInstance := keysign.NewTssKeySign(t.homeBase, t.p2pCommunication.GetLocalPeerID(), t.conf, t.priKey, t.p2pCommunication.BroadcastMsgChan, &t.stopChan, &t.Status.CurrKeySign, msgID)

	keygenMsgChannel, keygenSyncChannel := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignVerMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignSync, msgID, keygenSyncChannel)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignVerMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignSync, msgID)

	signatureData, err := keysignInstance.SignMessage(keySignReq)
	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keysign")
		atomic.AddUint64(&t.Status.FailedKeySign, 1)
		keySignFlag = common.Fail
		signatureData = &signing.SignatureData{}
	} else {
		atomic.AddUint64(&t.Status.SucKeySign, 1)
	}
	// this indicates we are not in this round keysign
	if signatureData == nil && err == nil {
		keysignInstance.WriteKeySignResult(w, "", "", common.NA)
		return
	}
	keysignInstance.WriteKeySignResult(w, base64.StdEncoding.EncodeToString(signatureData.R), base64.StdEncoding.EncodeToString(signatureData.S), keySignFlag)
}
