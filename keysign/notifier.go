package keysign

import (
	"bytes"
	"errors"
	"fmt"
	"sync"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	"github.com/gogo/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/peer"
)

// Notifier
type Notifier struct {
	MessageID    string
	keysignParty []peer.ID
	confirmLock  *sync.Mutex
	confirmList  map[peer.ID]*signing.SignatureData
	resp         chan *signing.SignatureData
}

// NewNotifier create a new instance of Notifier
func NewNotifier(messageID string, keysignParty []peer.ID) (*Notifier, error) {
	if len(messageID) == 0 {
		return nil, errors.New("messageID is empty")
	}
	if len(keysignParty) == 0 {
		return nil, errors.New("keysign party is empty")
	}
	return &Notifier{
		MessageID:    messageID,
		keysignParty: keysignParty,
		confirmLock:  &sync.Mutex{},
		confirmList:  make(map[peer.ID]*signing.SignatureData),
		resp:         make(chan *signing.SignatureData, 1),
	}, nil
}

// UpdateSignature is to update the signature
// return value bool , true indicated we already gather all the signature from keysign party, and they are all match
// false means we are still waiting for more signature from keysign party
func (n *Notifier) UpdateSignature(remotePeer peer.ID, data *signing.SignatureData) (bool, error) {
	n.confirmLock.Lock()
	defer n.confirmLock.Unlock()
	n.confirmList[remotePeer] = data
	finish, err := n.enoughSignature()
	if err != nil {
		n.resp <- nil
		return false, err
	}
	if finish {
		n.resp <- data
	}
	return finish, err
}

func (n *Notifier) enoughSignature() (bool, error) {
	for _, item := range n.keysignParty {
		_, ok := n.confirmList[item]
		if !ok {
			return false, nil
		}
	}
	var buf []byte
	// compare everyone with the first , they should be the same
	for _, s := range n.confirmList {
		if len(buf) == 0 {
			b, err := proto.Marshal(s)
			if err != nil {
				return false, fmt.Errorf("fail to marshal signing.SignatureData to byte:%w", err)
			}
			buf = b
			continue
		}
		sBuf, err := proto.Marshal(s)
		if err != nil {
			return false, fmt.Errorf("fail to marshal signing.SignatureData to bytes: %w", err)
		}
		if !bytes.Equal(buf, sBuf) {
			return false, errors.New("not the same signature")
		}
	}
	return true, nil
}

// GetResponseChannel the final signature gathered from keysign party will be returned from the channel
func (n *Notifier) GetResponseChannel() <-chan *signing.SignatureData {
	return n.resp
}
