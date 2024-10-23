package storage

import (
	"bytes"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/ioutil"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"

	"github.com/bnb-chain/tss-lib/ecdsa/keygen"
	"gitlab.com/thorchain/tss/go-tss/conversion"
)

const keyFragmentSeed = "TSS_FRAGMENT_SEED"
const addressBookName = "address_book"

type LocalPartySaveData interface {
}

// KeygenLocalState is a structure used to represent the data we saved locally for different keygen
type KeygenLocalState struct {
	PubKey          string   `json:"pub_key"`
	LocalData       []byte   `json:"local_data"`
	ParticipantKeys []string `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string   `json:"local_party_key"`
}

type KeygenLocalStateOld struct {
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
	SaveAddressBook(addressBook map[peer.ID][]maddr.Multiaddr) error
	RetrieveP2PAddresses() ([]maddr.Multiaddr, error)
}

// FileStateMgr save the local state to file
type FileStateMgr struct {
	folder      string
	writeLock   *sync.RWMutex
	encryptMode bool
	passkey     []byte
	keyGenState map[string]*KeygenLocalState
}

// NewFileStateMgr create a new instance of the FileStateMgr which implements LocalStateManager
func NewFileStateMgr(folder string, password string) (*FileStateMgr, error) {
	if len(folder) > 0 {
		_, err := os.Stat(folder)
		if err != nil && os.IsNotExist(err) {
			if err := os.MkdirAll(folder, os.ModePerm); err != nil {
				return nil, err
			}
		}
	}
	encryptMode := true
	key, err := getFragmentSeed(password)
	if err != nil {
		encryptMode = false
	}
	return &FileStateMgr{
		folder:      folder,
		writeLock:   &sync.RWMutex{},
		encryptMode: encryptMode,
		passkey:     key,
		keyGenState: map[string]*KeygenLocalState{},
	}, nil
}

func (fsm *FileStateMgr) getFilePathName(pubKey string) (string, error) {
	ret, err := conversion.CheckKeyOnCurve(pubKey)
	if err != nil {
		return "", err
	}
	if !ret {
		return "", errors.New("invalid pubkey for file name")
	}

	localFileName := fmt.Sprintf("localstate-%s.json", pubKey)
	if len(fsm.folder) > 0 {
		return filepath.Join(fsm.folder, localFileName), nil
	}
	return localFileName, nil
}

// SaveLocalState save the local state to file
func (fsm *FileStateMgr) SaveLocalState(state KeygenLocalState) error {
	buf, err := json.Marshal(state)
	if err != nil {
		return fmt.Errorf("fail to marshal KeygenLocalState to json: %w", err)
	}
	filePathName, err := fsm.getFilePathName(state.PubKey)
	if err != nil {
		return err
	}
	data, err := fsm.encryptFragment(buf)
	if err != nil {
		return err
	}
	return ioutil.WriteFile(filePathName, data, 0o600)
}

// GetLocalState read the local state from file system
func (fsm *FileStateMgr) GetLocalState(pubKey string) (KeygenLocalState, error) {
	if len(pubKey) == 0 {
		return KeygenLocalState{}, errors.New("pub key is empty")
	}
	fsm.writeLock.RLock()
	val, ok := fsm.keyGenState[pubKey]
	fsm.writeLock.RUnlock()
	if ok {
		return *val, nil
	}
	filePathName, err := fsm.getFilePathName(pubKey)
	if err != nil {
		return KeygenLocalState{}, err
	}
	if _, err := os.Stat(filePathName); os.IsNotExist(err) {
		return KeygenLocalState{}, err
	}
	filePathName = filepath.Clean(filePathName)

	buf, err := ioutil.ReadFile(filePathName)
	if err != nil {
		return KeygenLocalState{}, fmt.Errorf("fail to read from file(%s): %w", filePathName, err)
	}
	pt, err := fsm.decryptFragment(buf)
	if err != nil {
		return KeygenLocalState{}, fmt.Errorf("fail to decrypt data: %w", err)
	}
	var localState KeygenLocalState
	if err := json.Unmarshal(pt, &localState); nil != err {
		// try unmarshalling with the old format
		var localStateOld KeygenLocalStateOld
		if err := json.Unmarshal(pt, &localStateOld); nil != err {
			return KeygenLocalState{}, fmt.Errorf("fail to unmarshal KeygenLocalState with backwards compatibility: %w", err)
		}

		localState.PubKey = localStateOld.PubKey
		localState.ParticipantKeys = localStateOld.ParticipantKeys
		localState.LocalPartyKey = localStateOld.LocalPartyKey
		localState.LocalData, err = json.Marshal(localStateOld.LocalData)

		if err != nil {
			return KeygenLocalState{}, fmt.Errorf("fail to marshal KeygenLocalState.LocalData for backwards compatibility: %w", err)
		}
	}
	fsm.writeLock.Lock()
	defer fsm.writeLock.Unlock()
	fsm.keyGenState[pubKey] = &localState
	return localState, nil
}

func (fsm *FileStateMgr) SaveAddressBook(address map[peer.ID][]maddr.Multiaddr) error {
	if len(fsm.folder) < 1 {
		return errors.New("base file path is invalid")
	}
	filePathName := filepath.Join(fsm.folder, addressBookName)
	var buf bytes.Buffer

	for peer, addrs := range address {
		for _, addr := range addrs {
			// we do not save the loopback addr
			if strings.Contains(addr.String(), "127.0.0.1") {
				continue
			}
			record := addr.String() + "/p2p/" + peer.String() + "\n"
			_, err := buf.WriteString(record)
			if err != nil {
				return errors.New("fail to write the record to buffer")
			}
		}
	}
	fsm.writeLock.Lock()
	defer fsm.writeLock.Unlock()
	return os.WriteFile(filePathName, buf.Bytes(), 0o600)
}

// RetrieveP2PAddresses loads addresses from both the seed address book
// and state address book
func (fsm *FileStateMgr) RetrieveP2PAddresses() ([]maddr.Multiaddr, error) {
	if len(fsm.folder) < 1 {
		return nil, errors.New("base file path is invalid")
	}

	fsm.writeLock.RLock()
	defer fsm.writeLock.RUnlock()

	seedPath := filepath.Join(fsm.folder, "address_book.seed")
	seedAddresses, err := loadAddressBook(seedPath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}

	savePath := filepath.Join(fsm.folder, addressBookName)
	savedAddresses, err := loadAddressBook(savePath)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return seedAddresses, err
	}

	return append(seedAddresses, savedAddresses...), nil
}

func (fsm *FileStateMgr) encryptFragment(plainText []byte) ([]byte, error) {
	if !fsm.encryptMode {
		return plainText, nil
	}
	block, err := aes.NewCipher(fsm.passkey)
	if err != nil {
		return nil, err
	}
	// Creating GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Generating random nonce
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return nil, err
	}
	cipherText := gcm.Seal(nonce, nonce, plainText, nil)
	return cipherText, nil
}

func (fsm *FileStateMgr) decryptFragment(buf []byte) ([]byte, error) {
	if !fsm.encryptMode {
		return buf, nil
	}
	block, err := aes.NewCipher(fsm.passkey)
	if err != nil {
		return nil, err
	}
	// Creating GCM mode
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	// Detached nonce and decrypt
	nonce := buf[:gcm.NonceSize()]
	buf = buf[gcm.NonceSize():]
	plainText, err := gcm.Open(nil, nonce, buf, nil)
	if err != nil {
		return nil, err
	}
	return plainText, nil
}

func getFragmentSeed(password string) ([]byte, error) {
	seedStr := os.Getenv(keyFragmentSeed)
	if seedStr == "" {
		if password == "" {
			return nil, errors.New("empty fragment seed, please check password: " + password)
		}
		seedStr = password
	}

	h := sha256.New()
	h.Write([]byte(seedStr))
	seed := h.Sum(nil)
	return seed, nil
}

func loadAddressBook(path string) ([]maddr.Multiaddr, error) {
	path = filepath.Clean(path)
	_, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	input, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	data := strings.Split(string(input), "\n")
	var peerAddresses []maddr.Multiaddr
	for _, el := range data {
		// we skip the empty entry
		if len(el) == 0 {
			continue
		}
		addr, err := maddr.NewMultiaddr(el)
		if err != nil {
			return nil, fmt.Errorf("invalid address in address book: %w", err)
		}
		peerAddresses = append(peerAddresses, addr)
	}
	return peerAddresses, nil
}
