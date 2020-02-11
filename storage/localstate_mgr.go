package storage

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"os"
	"path/filepath"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
)

// KeygenLocalState is a structure used to represent the data we saved locally for different keygen
type KeygenLocalState struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

// LocalStateManager provide necessary methods to manage the local state, save it , and read it back
// LocalStateManager doesn't have any opinion in regards to where it should be persistent to
type LocalStateManager interface {
	SaveLocalState(state KeygenLocalState) error
	GetLocalState(pubKey string) (KeygenLocalState, error)
}

// FileStateMgr save the local state to file
type FileStateMgr struct {
	folder string
}

// NewFileStateMgr create a new instance of the FileStateMgr which implements LocalStateManager
func NewFileStateMgr(folder string) (*FileStateMgr, error) {
	if len(folder) > 0 {
		_, err := os.Stat(folder)
		if err != nil && os.IsNotExist(err) {
			if err := os.MkdirAll(folder, os.ModePerm); err != nil {
				return nil, err
			}
		}
	}
	return &FileStateMgr{folder: folder}, nil
}

func (fsm *FileStateMgr) getFilePathName(pubKey string) string {
	localFileName := fmt.Sprintf("localstate-%s.json", pubKey)
	if len(fsm.folder) > 0 {
		return filepath.Join(fsm.folder, localFileName)
	}
	return localFileName
}

// SaveLocalState save the local state to file
func (fsm *FileStateMgr) SaveLocalState(state KeygenLocalState) error {
	buf, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("fail to marshal KeygenLocalState to json: %w", err)
	}
	filePathName := fsm.getFilePathName(state.PubKey)
	return ioutil.WriteFile(filePathName, buf, 0655)
}

// GetLocalState read the local state from file system
func (fsm *FileStateMgr) GetLocalState(pubKey string) (KeygenLocalState, error) {
	if len(pubKey) == 0 {
		return KeygenLocalState{}, errors.New("pub key is empty")
	}
	filePathName := fsm.getFilePathName(pubKey)
	if _, err := os.Stat(filePathName); os.IsNotExist(err) {
		return KeygenLocalState{}, err
	}

	buf, err := ioutil.ReadFile(filePathName)
	if err != nil {
		return KeygenLocalState{}, fmt.Errorf("file to read from file(%s): %w", filePathName, err)
	}
	var localState KeygenLocalState
	if err := json.Unmarshal(buf, &localState); nil != err {
		return KeygenLocalState{}, fmt.Errorf("fail to unmarshal KeygenLocalState: %w", err)
	}
	return localState, nil
}
