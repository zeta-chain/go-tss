package main

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/json"
	"math/big"
	"os"

	"github.com/bnb-chain/tss-lib/ecdsa/keygen"
	"github.com/btcsuite/btcd/btcec/v2"
	coskey "github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bech32 "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"github.com/pkg/errors"
)

type (
	KeygenLocalState struct {
		PubKey          string                    `json:"pub_key"`
		LocalData       keygen.LocalPartySaveData `json:"local_data"`
		ParticipantKeys []string                  `json:"participant_keys"` // the participant of last key gen
		LocalPartyKey   string                    `json:"local_party_key"`
	}
)

func getTssSecretFile(file string) (KeygenLocalState, error) {
	_, err := os.Stat(file)
	if err != nil {
		return KeygenLocalState{}, err
	}

	buf, err := os.ReadFile(file)
	if err != nil {
		return KeygenLocalState{}, errors.Wrapf(err, "file to read from file %q", file)
	}

	var localState KeygenLocalState
	if err := json.Unmarshal(buf, &localState); nil != err {
		return KeygenLocalState{}, errors.Wrapf(err, "fail to unmarshal KeygenLocalState")
	}

	return localState, nil
}

func setupBech32Prefix() {
	config := sdk.GetConfig()
	// thorchain will import go-tss as a library , thus this is not needed, we copy the prefix here to avoid go-tss to import thorchain
	config.SetBech32PrefixForAccount("thor", "thorpub")
	config.SetBech32PrefixForValidator("thorv", "thorvpub")
	config.SetBech32PrefixForConsensusNode("thorc", "thorcpub")
}

func getTssPubKey(x, y *big.Int) (string, sdk.AccAddress, error) {
	if x == nil || y == nil {
		return "", sdk.AccAddress{}, errors.New("invalid points")
	}
	X := &btcec.FieldVal{}
	X.SetByteSlice(x.Bytes())
	Y := &btcec.FieldVal{}
	Y.SetByteSlice(y.Bytes())
	tssPubKey := btcec.NewPublicKey(X, Y)
	pubKeyCompressed := coskey.PubKey{
		Key: tssPubKey.SerializeCompressed(),
	}

	pubKey, err := bech32.MarshalPubKey(bech32.AccPK, &pubKeyCompressed)
	addr := sdk.AccAddress(pubKeyCompressed.Address().Bytes())
	return pubKey, addr, err
}

func aesCTRXOR(key, inText, iv []byte) ([]byte, error) {
	// AES-128 is selected due to size of encryptKey.
	aesBlock, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	stream := cipher.NewCTR(aesBlock, iv)
	outText := make([]byte, len(inText))
	stream.XORKeyStream(outText, inText)
	return outText, err
}

func generateRandomBytes(n int) ([]byte, error) {
	b := make([]byte, n)
	_, err := rand.Read(b)
	// Note that err == nil only if we read len(b) bytes.
	if err != nil {
		return nil, err
	}
	return b, nil
}
