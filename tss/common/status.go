package common

import (
	"encoding/json"
	"fmt"
	"github.com/binance-chain/tss-lib/crypto"
	"github.com/binance-chain/tss-lib/crypto/paillier"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"io/ioutil"
	"math/big"
	"os"
)

type Status byte

const (
	NA Status = iota
	Success
	Fail
)

// KeygenLocalStateItem
type KeygenLocalStateItem struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

// LoadLocalState from file
func LoadLocalState(filePathName string) (KeygenLocalStateItem, error) {
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
	var localState KeygenLocalStateItem
	if err := json.Unmarshal(buf, &localState); nil != err {
		return KeygenLocalStateItem{}, fmt.Errorf("fail to unmarshal KeygenLocalState: %w", err)
	}
	return localState, nil
}

func ProcessStateFile(sourceState KeygenLocalStateItem, parties []*btss.PartyID) (keygen.LocalPartySaveData, []*btss.PartyID) {
	var localKeyData keygen.LocalPartySaveData
	localKeyData = sourceState.LocalData
	var tempKs, tempNTildej, tempH1j, tempH2j []*big.Int
	var tempBigXj []*crypto.ECPoint
	var temPaillierPKs []*paillier.PublicKey

	for _, each := range parties {
		tempKs = append(tempKs, localKeyData.Ks[each.Index])
		tempNTildej = append(tempNTildej, localKeyData.NTildej[each.Index])
		tempH1j = append(tempH1j, localKeyData.H1j[each.Index])
		tempH2j = append(tempH2j, localKeyData.H2j[each.Index])
		tempBigXj = append(tempBigXj, localKeyData.BigXj[each.Index])
		temPaillierPKs = append(temPaillierPKs, localKeyData.PaillierPKs[each.Index])
	}

	keyData := keygen.LocalPartySaveData{
		LocalPreParams: keygen.LocalPreParams{
			PaillierSK: localKeyData.PaillierSK,
			NTildei:    localKeyData.NTildei,
			H1i:        localKeyData.H1i,
			H2i:        localKeyData.H2i,
		},
		LocalSecrets: keygen.LocalSecrets{
			Xi:      localKeyData.Xi,
			ShareID: localKeyData.ShareID,
		},
		Ks:          tempKs,
		NTildej:     tempNTildej,
		H1j:         tempH1j,
		H2j:         tempH2j,
		BigXj:       tempBigXj,
		PaillierPKs: temPaillierPKs,
		ECDSAPub:    localKeyData.ECDSAPub,
	}
	parties = btss.SortPartyIDs(parties)
	return keyData, parties
}
