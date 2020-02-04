package tss

import (
	"encoding/json"
	"net/http"
	"os"
	"sync/atomic"

	"github.com/binance-chain/go-sdk/common/types"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) Keygen(w http.ResponseWriter, r *http.Request) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()
	status := common.Success
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
	var keygenReq keygen.KeyGenReq
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	msgID, err := t.requestToMsgId(keygenReq)
	if err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	keygenInstance := keygen.NewTssKeyGen(t.homeBase, t.p2pCommunication.GetLocalPeerID(), t.conf, t.priKey, t.p2pCommunication.BroadcastMsgChan, &t.stopChan, t.preParams, &t.Status.CurrKeyGen, msgID)
	keygenMsgChannel, keygenSyncChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenVerMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenSync, msgID, keygenSyncChannel)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenVerMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenSync, msgID)

	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(keygenReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keygen")
		atomic.AddUint64(&t.Status.FailedKeyGen, 1)
	} else {
		atomic.AddUint64(&t.Status.SucKeyGen, 1)
	}

	newPubKey, addr, err := common.GetTssPubKey(k)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to generate the new Tss key")
		status = common.Fail
	}
	if os.Getenv("NET") == "testnet" || os.Getenv("NET") == "mocknet" {
		types.Network = types.TestNetwork
	}
	resp := keygen.KeyGenResp{
		PubKey:      newPubKey,
		PoolAddress: addr.String(),
		Status:      status,
		Blame:       keygenInstance.GetTssCommonStruct().BlamePeers,
	}
	buf, err := json.Marshal(resp)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(buf)
	if err != nil {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
