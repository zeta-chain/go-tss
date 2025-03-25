package p2p

import (
	"bufio"
	"bytes"
	"encoding/binary"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/logs"
)

const (
	TimeoutReadPayload  = 20 * time.Second
	TimeoutWritePayload = 20 * time.Second
	MaxPayload          = 20 * 1024 * 1024 // 20M
)

const unknown = "unknown"

// applyDeadline will be true , and only disable it when we are doing test
// the reason being the p2p network , mocknet, mock stream doesn't support SetReadDeadline ,SetWriteDeadline feature
var ApplyDeadline = &atomic.Bool{}

func init() {
	ApplyDeadline.Store(true)
}

type StreamManager struct {
	streams             map[string]streamItem
	maxAgeBeforeCleanup time.Duration
	mu                  sync.RWMutex
	logger              zerolog.Logger
}

type streamItem struct {
	msgID  string
	stream network.Stream
}

func NewStreamManager(logger zerolog.Logger) *StreamManager {
	// the max age before cleanup for unused streams
	const maxAgeBeforeCleanup = 1 * time.Minute

	sm := &StreamManager{
		streams:             make(map[string]streamItem),
		maxAgeBeforeCleanup: maxAgeBeforeCleanup,
		mu:                  sync.RWMutex{},
		logger:              logger,
	}

	ticker := time.NewTicker(sm.maxAgeBeforeCleanup)

	go func() {
		for {
			<-ticker.C
			sm.cleanup()
		}
	}()

	return sm
}

func (sm *StreamManager) Stash(msgID string, stream network.Stream) {
	if stream == nil {
		return
	}

	streamID := stream.ID()

	sm.mu.Lock()
	defer sm.mu.Unlock()

	// already exists
	if _, ok := sm.streams[streamID]; ok {
		return
	}

	// add stream items
	sm.streams[streamID] = streamItem{
		msgID:  msgID,
		stream: stream,
	}
}

func (sm *StreamManager) StashUnknown(stream network.Stream) {
	sm.Stash(unknown, stream)
}

func (sm *StreamManager) Free(msgID string) {
	var streamIDs []string

	sm.mu.RLock()
	for sid, streamItem := range sm.streams {
		if streamItem.msgID == msgID {
			streamIDs = append(streamIDs, sid)
		}
	}
	sm.mu.RUnlock()

	// noop
	if len(streamIDs) == 0 {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	for _, streamID := range streamIDs {
		sm.deleteStream(streamID)
	}
}

func (sm *StreamManager) FreeNow(msgID string, stream network.Stream) {
	if err := stream.Reset(); err != nil {
		sm.logger.Error().Err(err).
			Str(logs.MsgID, msgID).
			Stringer(logs.Peer, stream.Conn().RemotePeer()).
			Str("protocol", string(stream.Protocol())).
			Msg("Failed to reset the stream")
	}
}

// not thread safe
func (sm *StreamManager) deleteStream(streamID string) bool {
	s, ok := sm.streams[streamID]
	if !ok {
		return false
	}

	if err := s.stream.Reset(); err != nil {
		sm.logger.Error().Err(err).
			Str(logs.MsgID, s.msgID).
			Stringer(logs.Peer, s.stream.Conn().RemotePeer()).
			Str("protocol", string(s.stream.Protocol())).
			Msg("Failed to reset the stream")

		return false
	}

	delete(sm.streams, streamID)

	return true
}

func (sm *StreamManager) cleanup() {
	sm.mu.Lock()
	defer sm.mu.Unlock()

	var (
		totalStreams   = len(sm.streams)
		unknownStreams = 0
		freedStreams   = 0
		oldestStream   = time.Duration(0)
	)

	for streamID, streamItem := range sm.streams {
		var (
			s        = streamItem.stream
			lifespan = time.Since(s.Stat().Opened)
		)

		if streamItem.msgID == unknown {
			unknownStreams++
		}

		if lifespan > oldestStream {
			oldestStream = lifespan
		}

		// let's revisit later
		if lifespan <= sm.maxAgeBeforeCleanup {
			continue
		}

		if sm.deleteStream(streamID) {
			freedStreams++
		}
	}

	if freedStreams == 0 {
		return
	}

	lf := map[string]any{
		"streams.total_before":      totalStreams,
		"streams.total_after":       totalStreams - freedStreams,
		"streams.freed":             freedStreams,
		"streams.unknown_streams":   unknownStreams,
		"streams.oldest_stream_sec": oldestStream.Seconds(),
	}

	sm.logger.Info().Fields(lf).Msg("Stats for stashed streams")
}

// ReadStreamWithBuffer read data from the given stream
func ReadStreamWithBuffer(stream network.Stream) ([]byte, error) {
	if ApplyDeadline.Load() {
		deadline := time.Now().Add(TimeoutReadPayload)
		if err := stream.SetReadDeadline(deadline); err != nil {
			if errReset := stream.Reset(); errReset != nil {
				return nil, errReset
			}
			return nil, err
		}
	}

	streamReader := bufio.NewReader(stream)

	header := make([]byte, PayloadHeaderLen)
	n, err := io.ReadFull(streamReader, header)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read header from the stream (got %d bytes)", n)
	}

	payloadSize := binary.LittleEndian.Uint32(header)
	if payloadSize > MaxPayload {
		return nil, errors.Errorf("stream payload exceeded (got %d, max %d)", payloadSize, MaxPayload)
	}

	result := make([]byte, payloadSize)

	n, err = io.ReadFull(streamReader, result)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to read payload from the stream (got %d/%d bytes)", n, payloadSize)
	}

	return result, nil
}

// WriteStreamWithBuffer write the message to stream
func WriteStreamWithBuffer(msg []byte, stream network.Stream) error {
	if len(msg) > (MaxPayload - PayloadHeaderLen) {
		return errors.Errorf("payload size exceeded (got %d, max %d)", len(msg), MaxPayload)
	}

	if ApplyDeadline.Load() {
		deadline := time.Now().Add(TimeoutWritePayload)

		if err := stream.SetWriteDeadline(deadline); err != nil {
			if errReset := stream.Reset(); errReset != nil {
				return errors.Wrap(errReset, "failed to reset stream during failure in write deadline")
			}

			return errors.Wrap(err, "failed to set write deadline")
		}
	}

	// Create header containing the message length
	header := make([]byte, PayloadHeaderLen)
	msgLen := uint32(len(msg))
	binary.LittleEndian.PutUint32(header, msgLen)

	// Create buffer containing the header and message
	buf := bytes.NewBuffer(header)
	if _, err := buf.Write(msg); err != nil {
		return errors.Wrap(err, "failed to write message to buffer")
	}

	n, err := stream.Write(buf.Bytes())
	if err != nil {
		return errors.Wrapf(err, "stream write failed (wrote %d/%d bytes)", n, buf.Len())
	}

	return nil
}
