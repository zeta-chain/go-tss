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
	LengthHeader        = 4 // LengthHeader represent how many bytes we used as header
	TimeoutReadPayload  = 20 * time.Second
	TimeoutWritePayload = 20 * time.Second
	MaxPayload          = 20 * 1024 * 1024 // 20M
)

// applyDeadline will be true , and only disable it when we are doing test
// the reason being the p2p network , mocknet, mock stream doesn't support SetReadDeadline ,SetWriteDeadline feature
var ApplyDeadline = &atomic.Bool{}

func init() {
	ApplyDeadline.Store(true)
}

type StreamMgr struct {
	unusedStreams map[string][]network.Stream
	mu            *sync.RWMutex
	logger        zerolog.Logger
}

func NewStreamMgr(logger zerolog.Logger) *StreamMgr {
	return &StreamMgr{
		unusedStreams: make(map[string][]network.Stream),
		mu:            &sync.RWMutex{},
		logger:        logger.With().Str(logs.Component, "stream_manager").Logger(),
	}
}

func (sm *StreamMgr) ReleaseStream(msgID string) {
	sm.mu.RLock()
	usedStreams, okStream := sm.unusedStreams[msgID]
	unknownStreams, okUnknown := sm.unusedStreams["UNKNOWN"]
	streams := append(usedStreams, unknownStreams...)
	sm.mu.RUnlock()

	// noop
	if !(okStream || okUnknown) {
		return
	}

	for _, stream := range streams {
		if err := stream.Reset(); err != nil {
			sm.logger.Error().Err(err).
				Str(logs.MsgID, msgID).
				Str("stream_id", stream.ID()).
				Msg("Failed to reset the stream")
		}
	}

	sm.mu.Lock()
	delete(sm.unusedStreams, msgID)
	delete(sm.unusedStreams, "UNKNOWN")
	sm.mu.Unlock()
}

func (sm *StreamMgr) AddStream(msgID string, stream network.Stream) {
	if stream == nil {
		return
	}

	sm.mu.Lock()
	defer sm.mu.Unlock()

	entries, ok := sm.unusedStreams[msgID]
	if !ok {
		sm.unusedStreams[msgID] = []network.Stream{stream}
		return
	}

	sm.unusedStreams[msgID] = append(entries, stream)
}

// ReadStreamWithBuffer read data from the given stream
func ReadStreamWithBuffer(stream network.Stream) ([]byte, error) {
	if ApplyDeadline.Load() {
		if err := stream.SetReadDeadline(time.Now().Add(TimeoutReadPayload)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return nil, errReset
			}
			return nil, err
		}
	}

	streamReader := bufio.NewReader(stream)

	header := make([]byte, LengthHeader)
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
	const uint32Size = 4
	if len(msg) > (MaxPayload - uint32Size) {
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
	header := make([]byte, LengthHeader)
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
