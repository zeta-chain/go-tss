package keysign

import (
	"context"
	"sync"
	"time"

	"github.com/bnb-chain/tss-lib/common"
	"github.com/golang/protobuf/proto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
)

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
	streamMgr    *p2p.StreamManager
}

// NewSignatureNotifier create a new instance of SignatureNotifier
func NewSignatureNotifier(host host.Host, logger zerolog.Logger) *SignatureNotifier {
	logger = logger.With().
		Str(logs.Component, "signature_notifier").
		Stringer(logs.Host, host.ID()).
		Logger()

	ctx, cancel := context.WithCancel(context.Background())

	s := &SignatureNotifier{
		ctx:          ctx,
		cancel:       cancel,
		host:         host,
		notifierLock: &sync.Mutex{},
		notifiers:    make(map[string]*notifier),
		streamMgr:    p2p.NewStreamManager(logger),
		logger:       logger,
	}

	host.SetStreamHandler(p2p.ProtocolSignatureNotifier, s.handleStream)

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
	logger := s.logger.With().
		Stringer(logs.Peer, stream.Conn().RemotePeer()).
		Logger()

	logger.Debug().Msg("reading signature notifier message")

	payload, err := p2p.ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msg("fail to read payload from stream")
		s.streamMgr.StashUnknown(stream)
		return
	}

	// we tell the sender we have received the message
	err = p2p.WriteStreamWithBuffer([]byte(p2p.ResponseMessage), stream)
	if err != nil {
		logger.Error().Err(err).Msg("Fail to write the reply to peer")
	}

	var msg messages.KeysignSignature
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		s.streamMgr.StashUnknown(stream)
		return
	}

	s.streamMgr.Stash(msg.ID, stream)

	success := msg.KeysignStatus == messages.KeysignSignature_Success && len(msg.Signatures) > 0
	if !success {
		return
	}

	var signatures []*common.SignatureData
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

func (s *SignatureNotifier) sendOneMsgToPeer(m *signatureItem) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
	defer cancel()

	stream, err := s.host.NewStream(ctx, m.peerID, p2p.ProtocolSignatureNotifier)
	if err != nil {
		return errors.Wrapf(err, "unable to create %s stream to peer", p2p.ProtocolSignatureNotifier)
	}

	defer s.streamMgr.Stash(m.messageID, stream)

	s.logger.Debug().Stringer(logs.Peer, m.peerID).Msg("opened stream to peer successfully")

	ks := &messages.KeysignSignature{
		ID:            m.messageID,
		KeysignStatus: messages.KeysignSignature_Failed,
	}

	if m.signatureData != nil {
		var signatures [][]byte
		for _, el := range m.signatureData {
			buf, err := proto.Marshal(el)
			if err != nil {
				return errors.Wrapf(err, "fail to marshal signature data to bytes")
			}

			signatures = append(signatures, buf)
		}

		ks.Signatures = signatures
		ks.KeysignStatus = messages.KeysignSignature_Success
	}

	ksBuf, err := proto.Marshal(ks)
	if err != nil {
		return errors.Wrapf(err, "fail to marshal KeysignSignature")
	}

	err = p2p.WriteStreamWithBuffer(ksBuf, stream)
	if err != nil {
		return errors.Wrapf(err, "fail to write KeysignSignature to stream")
	}

	// we wait for 1 second to allow the receive notify us
	if err := stream.SetReadDeadline(time.Now().Add(time.Second * 1)); nil != err {
		return errors.Wrapf(err, "fail to set read deadline to stream")
	}

	const expectedResponseSize = p2p.PayloadHeaderLen + p2p.ResponseMessageBytesLen

	ack := make([]byte, expectedResponseSize)
	if _, err := stream.Read(ack); err != nil {
		return errors.Wrapf(err, "failed to read response from stream")
	}

	return err
}

// BroadcastSignature sending the keysign signature to all other peers
func (s *SignatureNotifier) BroadcastSignature(messageID string, sig []*common.SignatureData, peers []peer.ID) error {
	return s.broadcastCommon(messageID, sig, peers)
}

func (s *SignatureNotifier) broadcastCommon(messageID string, sig []*common.SignatureData, peers []peer.ID) error {
	var wg sync.WaitGroup

	for _, peerID := range peers {
		// don't send the signature to itself
		if peerID == s.host.ID() {
			continue
		}

		sig := &signatureItem{
			messageID:     messageID,
			peerID:        peerID,
			signatureData: sig,
		}

		wg.Add(1)
		go func() {
			defer wg.Done()

			if err := s.sendOneMsgToPeer(sig); err != nil {
				s.logger.Error().Err(err).
					Str(logs.MsgID, sig.messageID).
					Stringer(logs.Peer, sig.peerID).
					Msg("Failed to send signature to peer")
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
		return nil, errors.Wrapf(err, "unable to create or update notifier")
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
		return nil, errors.Errorf("timeout: didn't receive signature after %s", timeout.String())
	case <-sigChan:
		return nil, p2p.ErrSigGenerated
	}
}

func (s *SignatureNotifier) FreeStreams(msgID string) {
	s.streamMgr.Free(msgID)
}
