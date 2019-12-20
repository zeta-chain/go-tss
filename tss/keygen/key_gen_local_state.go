package keygen

import (
	"encoding/json"
	"fmt"
	"gitlab.com/thorchain/tss/go-tss/tss/common"
	"io/ioutil"
	"path/filepath"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
)

// KeyGenRequest
type KeyGenReq struct {
	Keys []string `json:"keys"`
}

// KeyGenResponse
type KeyGenResp struct {
	PubKey     string        `json:"pub_key"`
	BNBAddress string        `json:"bnb_address"`
	Status     common.Status `json:"status"`
}

func SaveLocalStateToFile(filePathName string, state common.KeygenLocalStateItem) error {
	buf, err := json.Marshal(state)
	if nil != err {
		return fmt.Errorf("fail to marshal KeygenLocalState to json: %w", err)
	}
	return ioutil.WriteFile(filePathName, buf, 0655)
}

func (keygen *TssKeyGen) AddLocalPartySaveData(homeBase string, data keygen.LocalPartySaveData, keyGenLocalStateItem common.KeygenLocalStateItem) error {
	pubKey, addr, err := common.GetTssPubKey(data.ECDSAPub)
	if nil != err {
		return fmt.Errorf("fail to get thorchain pubkey: %w", err)
	}
	keygen.logger.Debug().Msgf("pubkey: %s, bnb address: %s", pubKey, addr)
	keyGenLocalStateItem.PubKey = pubKey
	keyGenLocalStateItem.LocalData = data
	localFileName := fmt.Sprintf("localstate-%s.json", pubKey)
	if len(homeBase) > 0 {
		localFileName = filepath.Join(homeBase, localFileName)
	}
	return SaveLocalStateToFile(localFileName, keyGenLocalStateItem)
}
