package go_tss

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
)

// KeygenLocalState is used to save the keygen result into local files
type KeygenLocalState []KeygenLocalStateItem

// KeygenLocalStateItem
type KeygenLocalStateItem struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

// GetLocalState from file
func GetLocalState(filePathName string) (KeygenLocalState, error) {
	if len(filePathName) == 0 {
		return KeygenLocalState{}, nil
	}

	if _, err := os.Stat(filePathName); os.IsNotExist(err) {
		return KeygenLocalState{}, nil
	}

	buf, err := ioutil.ReadFile(filePathName)
	if nil != err {
		return KeygenLocalState{}, fmt.Errorf("file to read from file(%s): %w", filePathName, err)
	}
	var kLocalState KeygenLocalState
	if err := json.Unmarshal(buf, &kLocalState); nil != err {
		return KeygenLocalState{}, fmt.Errorf("fail to unmarshal KeygenLocalState: %w", err)
	}
	return kLocalState, nil
}

func SaveLocalStateToFile(filePathName string, state KeygenLocalState) error {
	buf, err := json.Marshal(state)
	if nil != err {
		return fmt.Errorf("fail to marshal KeygenLocalState to json: %w", err)
	}
	return ioutil.WriteFile(filePathName, buf, 0655)
}
