package keysign

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	"github.com/golang/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/messages"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const signatureNotifiers = 10

var signatureNotifierProtocol protocol.ID = "/p2p/signatureNotifier"

type signatureItem struct {
	messageID     string
	peerID        peer.ID
	signatureData *signing.SignatureData
}

// SignatureNotifier is design to notify the
type SignatureNotifier struct {
	logger       zerolog.Logger
	host         host.Host
	stopChan     chan struct{}
	notifierLock *sync.Mutex
	notifiers    map[string]*Notifier
	messages     chan *signatureItem
	wg           *sync.WaitGroup
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host) *SignatureNotifier {
	s := &SignatureNotifier{
		logger:       log.With().Str("module", "signature_notifier").Logger(),
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*Notifier),
		stopChan:     make(chan struct{}),
		messages:     make(chan *signatureItem),
		wg:           &sync.WaitGroup{},
	}
	host.SetStreamHandler(signatureNotifierProtocol, s.handleStream)
	return s
}

// HandleStream handle signature notify stream
func (s *SignatureNotifier) handleStream(stream network.Stream) {
	defer func() {
		if err := stream.Close(); err != nil {
			s.logger.Err(err).Msg("fail to close the stream")
		}
	}()
	remotePeer := stream.Conn().RemotePeer()
	logger := s.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading signature notifier message")
	length, err := p2p.ReadLength(stream)
	if err != nil {
		logger.Err(err).Msg("fail to read length header from stream")
		return
	}
	payload, err := p2p.ReadPayload(stream, length)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		return
	}
	_ = payload
	var msg messages.KeysignSignature
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		return
	}
	var signature signing.SignatureData
	if err := proto.Unmarshal(msg.Signature, &signature); err != nil {
		logger.Error().Err(err).Msg("fail to unmarshal signature data")
		return
	}
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	n, ok := s.notifiers[msg.ID]
	if !ok {
		logger.Error().Msgf("notifier for message id(%s) not exist", msg.ID)
		return
	}
	finished, err := n.UpdateSignature(remotePeer, &signature)
	if err != nil {
		logger.Error().Err(err).Msg("fail to update local signature data")
		delete(s.notifiers, msg.ID)
		return
	}
	if finished {
		delete(s.notifiers, msg.ID)
	}
}

func (s *SignatureNotifier) Start() error {
	for i := 0; i < signatureNotifiers; i++ {
		s.wg.Add(1)
		go s.sendMessageToPeer()
	}
	return nil
}

// Stop the signature notifier
func (s *SignatureNotifier) Stop() error {
	close(s.stopChan)
	s.wg.Wait()
	return nil
}

func (s *SignatureNotifier) sendMessageToPeer() {
	s.logger.Debug().Msg("start to send message to peers")
	defer s.logger.Debug().Msg("stop send message to peers")
	defer s.wg.Done()
	for {
		select {
		case <-s.stopChan:
			return
		case msg := <-s.messages:
			if err := s.sendOneMsgToPeer(msg); err != nil {
				s.logger.Error().Err(err).Msgf("fail to send message(%s) to peer:%s", msg.messageID, msg.peerID)
			}
		}
	}
}

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	stream, err := s.host.NewStream(ctx, m.peerID, signatureNotifierProtocol)
	if err != nil {
		return fmt.Errorf("fail to create stream to peer(%s):%w", m.peerID, err)
	}
	s.logger.Info().Msgf("open stream to (%s) successfully", m.peerID)
	defer func() {
		if err := stream.Close(); err != nil {
			s.logger.Error().Err(err).Msg("fail to close stream")
		}
	}()
	buf, err := proto.Marshal(m.signatureData)
	if err != nil {
		return fmt.Errorf("fail to marshal signature data to bytes:%w", err)
	}
	ks := &messages.KeysignSignature{
		ID:        m.messageID,
		Signature: buf,
	}
	ksBuf, err := proto.Marshal(ks)
	if err != nil {
		return fmt.Errorf("fail to marshal Keysign Signature to bytes:%w", err)
	}

	if err := p2p.WriteLength(stream, uint32(len(ksBuf))); err != nil {
		return err
	}
	_, err = stream.Write(ksBuf)
	if err != nil {
		if errReset := stream.Reset(); errReset != nil {
			return errReset
		}
		return fmt.Errorf("fail to write message to stream:%w", err)
	}
	return nil
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(messageID string, sig *signing.SignatureData, peers []peer.ID) error {
	for _, p := range peers {
		select {
		case s.messages <- &signatureItem{
			messageID:     messageID,
			peerID:        p,
			signatureData: sig,
		}:
		case <-s.stopChan:
			return nil
		}
	}
	return nil
}

func (s *SignatureNotifier) addToNotifiers(n *Notifier) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	s.notifiers[n.MessageID] = n
}

func (s *SignatureNotifier) removeNotifier(n *Notifier) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	delete(s.notifiers, n.MessageID)
}

// WaitForSignature wait until keysign finished and signature is available
func (s *SignatureNotifier) WaitForSignature(messageID string, peers []peer.ID, timeout time.Duration) (*signing.SignatureData, error) {
	n, err := NewNotifier(messageID, peers)
	if err != nil {
		return nil, fmt.Errorf("fail to create notifier")
	}
	s.addToNotifiers(n)
	defer s.removeNotifier(n)

	select {
	case d := <-n.GetResponseChannel():
		return d, nil
	case <-s.stopChan:
		return nil, errors.New("request to exit")
	case <-time.After(timeout):
		return nil, fmt.Errorf("timeout: didn't receive signature after %s", timeout)
	}
}
