package go_tss

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
)

// KeygenLocalStateItem
type KeygenLocalStateItem struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

// GetLocalState from file
func GetLocalState(filePathName string) (KeygenLocalStateItem, error) {
	if len(filePathName) == 0 {
		return KeygenLocalStateItem{}, nil
	}
	if _, err := os.Stat(filePathName); os.IsNotExist(err) {
		return KeygenLocalStateItem{}, nil
	}

	buf, err := ioutil.ReadFile(filePathName)
	if nil != err {
		return KeygenLocalStateItem{}, fmt.Errorf("file to read from file(%s): %w", filePathName, err)
	}
	var LocalState KeygenLocalStateItem
	if err := json.Unmarshal(buf, &LocalState); nil != err {
		return KeygenLocalStateItem{}, fmt.Errorf("fail to unmarshal KeygenLocalState: %w", err)
	}
	return LocalState, nil
}

func SaveLocalStateToFile(filePathName string, state KeygenLocalStateItem) error {
	buf, err := json.Marshal(state)
	if nil != err {
		return fmt.Errorf("fail to marshal KeygenLocalState to json: %w", err)
	}
	return ioutil.WriteFile(filePathName, buf, 0655)
}
