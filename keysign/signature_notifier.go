package keysign

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/bnb-chain/tss-lib/common"
	"github.com/golang/protobuf/proto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
)

var signatureNotifierProtocol protocol.ID = "/p2p/signatureNotifier"

type signatureItem struct {
	messageID     string
	peerID        peer.ID
	signatureData []*common.SignatureData
}

// SignatureNotifier is design to notify the
type SignatureNotifier struct {
	ctx       context.Context
	cancel    context.CancelFunc
	startOnce sync.Once

	logger       zerolog.Logger
	host         host.Host
	notifierLock *sync.Mutex
	notifiers    map[string]*notifier
	streamMgr    *p2p.StreamMgr
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host, logger zerolog.Logger) *SignatureNotifier {
	logger = logger.With().Str(logs.Component, "signature_notifier").Logger()

	ctx, cancel := context.WithCancel(context.Background())

	s := &SignatureNotifier{
		ctx:          ctx,
		cancel:       cancel,
		logger:       logger,
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*notifier),
		streamMgr:    p2p.NewStreamMgr(),
	}

	host.SetStreamHandler(signatureNotifierProtocol, s.handleStream)

	return s
}

// Start launches a background cleanup goroutine
func (s *SignatureNotifier) Start() {
	s.startOnce.Do(func() {
		go s.cleanupStaleNotifiers()
	})
}

// Stop stops the background cleanup goroutine
func (s *SignatureNotifier) Stop() {
	s.cancel()
}

// cleanupStaleNotifiers will periodically check any active notifiers to see if they've been around
// for longer than notifierTTL. This was added because we allow broadcasts to create notifier objects
// in handleStream, and need a way to cleanup notifiers that went unused
func (s *SignatureNotifier) cleanupStaleNotifiers() {
	doCleanup := func() {
		s.notifierLock.Lock()
		for messageID, notifier := range s.notifiers {
			if time.Since(notifier.lastUpdated) > notifier.ttl {
				delete(s.notifiers, messageID)
			}
		}
		s.notifierLock.Unlock()
	}
	ticker := time.NewTicker(time.Second * 15)
	defer ticker.Stop()

	// quickly do an initial cleanup instead of waiting for the ticker. this aids
	// in testing so we don't have to wait for the ticker to fire
	doCleanup()

	for {
		select {
		case <-s.ctx.Done():
			return
		case <-ticker.C:
			doCleanup()
		}
	}
}

// HandleStream handle signature notify stream
func (s *SignatureNotifier) handleStream(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()
	logger := s.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading signature notifier message")
	payload, err := p2p.ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		s.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	// we tell the sender we have received the message
	err = p2p.WriteStreamWithBuffer([]byte("done"), stream)
	if err != nil {
		logger.Error().Err(err).Msgf("fail to write the reply to peer: %s", remotePeer)
	}
	var msg messages.KeysignSignature
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		s.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
	s.streamMgr.AddStream(msg.ID, stream)
	var signatures []*common.SignatureData
	if len(msg.Signatures) > 0 && msg.KeysignStatus == messages.KeysignSignature_Success {
		for _, el := range msg.Signatures {
			var signature common.SignatureData
			if err := proto.Unmarshal(el, &signature); err != nil {
				logger.Error().Err(err).Msg("fail to unmarshal signature data")
				return
			}
			signatures = append(signatures, &signature)
		}

		_, err = s.createOrUpdateNotifier(msg.ID, nil, "", signatures, defaultNotifierTTL)
		if err != nil {
			s.logger.Error().Err(err).Msg("fail to update notifier")
		}
	}
}

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()
	stream, err := s.host.NewStream(ctx, m.peerID, signatureNotifierProtocol)
	if err != nil {
		return fmt.Errorf("fail to create stream to peer(%s):%w", m.peerID, err)
	}
	s.logger.Debug().Msgf("open stream to (%s) successfully", m.peerID)
	defer func() {
		s.streamMgr.AddStream(m.messageID, stream)
	}()
	ks := &messages.KeysignSignature{
		ID:            m.messageID,
		KeysignStatus: messages.KeysignSignature_Failed,
	}

	if m.signatureData != nil {
		var signatures [][]byte
		for _, el := range m.signatureData {
			buf, err := proto.Marshal(el)
			if err != nil {
				return fmt.Errorf("fail to marshal signature data to bytes:%w", err)
			}
			signatures = append(signatures, buf)
		}
		ks.Signatures = signatures
		ks.KeysignStatus = messages.KeysignSignature_Success
	}
	ksBuf, err := proto.Marshal(ks)
	if err != nil {
		return fmt.Errorf("fail to marshal Keysign Signature to bytes:%w", err)
	}
	err = p2p.WriteStreamWithBuffer(ksBuf, stream)
	if err != nil {
		return fmt.Errorf("fail to write message to stream:%w", err)
	}
	// we wait for 1 second to allow the receive notify us
	if err := stream.SetReadDeadline(time.Now().Add(time.Second * 1)); nil != err {
		return err
	}
	ret := make([]byte, 8)
	_, err = stream.Read(ret)
	return err
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(
	messageID string,
	sig []*common.SignatureData,
	peers []peer.ID,
) error {
	return s.broadcastCommon(messageID, sig, peers)
}

func (s *SignatureNotifier) broadcastCommon(
	messageID string,
	sig []*common.SignatureData,
	peers []peer.ID,
) error {
	wg := sync.WaitGroup{}
	for _, p := range peers {
		if p == s.host.ID() {
			// don't send the signature to itself
			continue
		}
		signature := &signatureItem{
			messageID:     messageID,
			peerID:        p,
			signatureData: sig,
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			err := s.sendOneMsgToPeer(signature)
			if err != nil {
				s.logger.Error().Err(err).Msgf("fail to send signature to peer %s", signature.peerID.String())
			}
		}()
	}
	wg.Wait()
	return nil
}

// BroadcastFailed will send keysign failed message to the nodes that are not in the keysign party
func (s *SignatureNotifier) BroadcastFailed(messageID string, peers []peer.ID) error {
	return s.broadcastCommon(messageID, nil, peers)
}

func (s *SignatureNotifier) removeNotifier(n *notifier) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	delete(s.notifiers, n.messageID)
}

// createOrUpdateNotifier will create a new notifier object for the provided messageID if one does
// not exist, or retrieve one that has already been created. it will also update any unset
// messages/poolPubKey/signatures to provided values that are not empty/nil. this is to allow incremental
// assembly of the required components in *any order*. for example, we could received a broadcast of
// signatures from another peer before our node begins to wait for signatures. in this case, we should
// hold on to the signatures until this node enters WaitForSignatures. alternatively, we may begin to
// WaitForSignature first, in which case we should wait for signatures via broadcast. once all components
// are set (see readyToProcess()), we can process the signature
func (s *SignatureNotifier) createOrUpdateNotifier(
	messageID string,
	messages [][]byte,
	poolPubKey string,
	signatures []*common.SignatureData,
	timeout time.Duration,
) (*notifier, error) {
	s.notifierLock.Lock()
	defer s.notifierLock.Unlock()
	n, ok := s.notifiers[messageID]
	if !ok {
		var err error
		n, err = newNotifier(messageID, messages, poolPubKey, signatures)
		if err != nil {
			return nil, err
		}
		s.notifiers[messageID] = n
	}
	n.ttl = timeout

	// update any fields that were not set previously
	n.updateUnset(messages, poolPubKey, signatures)

	if n.readyToProcess() {
		err := n.processSignature(n.signatures)
		if err != nil {
			return nil, err
		}
	}
	return n, nil
}

// WaitForSignature wait until keysign finished and signature is available
func (s *SignatureNotifier) WaitForSignature(
	messageID string,
	message [][]byte,
	poolPubKey string,
	timeout time.Duration,
	sigChan chan string,
) ([]*common.SignatureData, error) {
	s.logger.Debug().Msg("waiting for signature")
	n, err := s.createOrUpdateNotifier(messageID, message, poolPubKey, nil, timeout+time.Second)
	if err != nil {
		return nil, fmt.Errorf("unable to create or update notifier: %v", err)
	}

	// only remove the notifier here, where it is ultimately consumed regardless
	// of where it was created
	defer s.removeNotifier(n)

	select {
	case d := <-n.getResponseChannel():
		s.logger.Debug().Msg("got signature from peer")
		return d, nil
	case <-time.After(timeout):
		s.logger.Debug().Msg("timed out waiting for signature from peer")
		return nil, fmt.Errorf("timeout: didn't receive signature after %s", timeout)
	case <-sigChan:
		s.logger.Debug().Msg("got signature generated signal")
		return nil, p2p.ErrSigGenerated
	}
}

func (s *SignatureNotifier) ReleaseStream(msgID string) {
	s.streamMgr.ReleaseStream(msgID)
}
