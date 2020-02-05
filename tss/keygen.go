package tss

import (
	"os"
	"sync/atomic"

	"github.com/binance-chain/go-sdk/common/types"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func (t *TssServer) Keygen(req keygen.KeyGenReq) (keygen.KeyGenResp, error) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()

	status := common.Success

	msgID, err := t.requestToMsgId(req)
	if err != nil {
		return keygen.KeyGenResp{}, err
	}

	keygenInstance := keygen.NewTssKeyGen(
		t.homeBase,
		t.p2pCommunication.GetLocalPeerID(),
		t.conf,
		t.priKey,
		t.p2pCommunication.BroadcastMsgChan,
		&t.stopChan,
		t.preParams,
		&t.Status.CurrKeyGen,
		msgID,
	)

	keygenMsgChannel, keygenSyncChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenVerMsg, msgID, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenSync, msgID, keygenSyncChannel)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenVerMsg, msgID)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenSync, msgID)

	// the statistic of keygen only care about Tss it self, even if the
	// following http response aborts, it still counted as a successful keygen
	// as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(req)
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

	return keygen.NewKeyGenResp(
		newPubKey,
		addr.String(),
		status,
		keygenInstance.GetTssCommonStruct().BlamePeers,
	), nil
}
