package keysign

import (
	"crypto/ecdsa"
	"errors"
	"fmt"
	"math/big"
	"time"

	"github.com/bnb-chain/tss-lib/v2/common"
	"github.com/cometbft/cometbft/crypto/ed25519"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"github.com/decred/dcrd/dcrec/edwards/v2"
	"github.com/tendermint/btcd/btcec"
)

const defaultNotifierTTL = time.Second * 30

// notifier is design to receive keysign signature, success or failure
type notifier struct {
	messageID   string
	messages    [][]byte // the message
	poolPubKey  string
	signatures  []*common.SignatureData
	resp        chan []*common.SignatureData
	processed   bool
	lastUpdated time.Time
	ttl         time.Duration
}

// newNotifier create a new instance of notifier.
func newNotifier(messageID string, messages [][]byte, poolPubKey string, signatures []*common.SignatureData) (*notifier, error) {
	if len(messageID) == 0 {
		return nil, errors.New("messageID is empty")
	}
	return &notifier{
		messageID:   messageID,
		messages:    messages,
		poolPubKey:  poolPubKey,
		signatures:  signatures,
		resp:        make(chan []*common.SignatureData, 1),
		lastUpdated: time.Now(),
		ttl:         defaultNotifierTTL,
	}, nil
}

// readyToProcess ensures we have everything we need to process the signatures
func (n *notifier) readyToProcess() bool {
	return len(n.messageID) > 0 &&
		len(n.messages) > 0 &&
		len(n.poolPubKey) > 0 &&
		len(n.signatures) > 0 &&
		!n.processed
}

// updateUnset will incrementally update the internal state of notifier with any new values
// provided that are not nil/empty.
func (n *notifier) updateUnset(messages [][]byte, poolPubKey string, signatures []*common.SignatureData) {
	n.lastUpdated = time.Now()
	if n.messages == nil {
		n.messages = messages
	}
	if len(n.poolPubKey) == 0 {
		n.poolPubKey = poolPubKey
	}
	if n.signatures == nil {
		n.signatures = signatures
	}
}

// verifySignature is a method to verify the signature against the message it signed , if the signature can be verified successfully
// There is a method call VerifyBytes in crypto.PubKey, but we can't use that method to verify the signature, because it always hash the message
// first and then verify the hash of the message against the signature , which is not the case in tss
// go-tss respect the payload it receives , assume the payload had been hashed already by whoever send it in.
func (n *notifier) verifySignature(data *common.SignatureData, msg []byte) error {
	// we should be able to use any of the pubkeys to verify the signature
	pubKey, err := sdk.UnmarshalPubKey(sdk.AccPK, n.poolPubKey)
	if err != nil {
		return fmt.Errorf("fail to get pubkey from bech32 pubkey string(%s):%w", n.poolPubKey, err)
	}

	switch pubKey.Type() {
	case secp256k1.KeyType:
		pub, err := btcec.ParsePubKey(pubKey.Bytes(), btcec.S256())
		if err != nil {
			return err
		}
		verified := ecdsa.Verify(pub.ToECDSA(), msg, new(big.Int).SetBytes(data.R), new(big.Int).SetBytes(data.S))
		if !verified {
			return fmt.Errorf("signature did not verify")
		}
		return nil

	case ed25519.KeyType:
		bPk, err := edwards.ParsePubKey(pubKey.Bytes())
		if err != nil {
			return fmt.Errorf("invalid ed25519 key with error %w", err)
		}
		newSig, err := edwards.ParseSignature(data.Signature)
		if err != nil {
			return err
		}

		verified := edwards.Verify(bPk, msg, newSig.R, newSig.S)
		if !verified {
			return fmt.Errorf("signature did not verify")
		}
		return nil
	default:
		return errors.New("invalid pubkey type")
	}

}

// processSignature is to verify whether the signature is valid
// return value bool , true indicated we already gather all the signature from keysign party, and they are all match
// false means we are still waiting for more signature from keysign party
func (n *notifier) processSignature(data []*common.SignatureData) error {
	// only need to verify the signature when data is not nil
	// when data is nil , which means keysign  failed, there is no signature to be verified in that case
	// for gg20, it wrap the signature R,S into ECSignature structure
	if len(data) != 0 {
		for i := 0; i < len(data); i++ {
			eachSig := data[i]
			msg := n.messages[i]
			if eachSig.GetSignature() != nil {
				err := n.verifySignature(eachSig, msg)
				if err != nil {
					return fmt.Errorf("error verifying signature (%d of %d) %x: %v",
						i, len(data), eachSig.Signature, err)
				}
			} else {
				return fmt.Errorf("keysign failed with nil signature")
			}
		}
		n.processed = true
		n.resp <- data
		return nil
	}

	return nil
}

// getResponseChannel the final signature gathered from keysign party will be returned from the channel
func (n *notifier) getResponseChannel() <-chan []*common.SignatureData {
	return n.resp
}
