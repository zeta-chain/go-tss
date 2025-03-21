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
	"github.com/rs/zerolog/log"
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
	streamLocker  *sync.RWMutex
	logger        zerolog.Logger
}

func NewStreamMgr() *StreamMgr {
	return &StreamMgr{
		unusedStreams: make(map[string][]network.Stream),
		streamLocker:  &sync.RWMutex{},
		logger:        log.With().Str("module", "communication").Logger(),
	}
}

func (sm *StreamMgr) ReleaseStream(msgID string) {
	sm.streamLocker.RLock()
	usedStreams, okStream := sm.unusedStreams[msgID]
	unknownStreams, okUnknown := sm.unusedStreams["UNKNOWN"]
	streams := append(usedStreams, unknownStreams...)
	sm.streamLocker.RUnlock()
	if okStream || okUnknown {
		for _, el := range streams {
			err := el.Reset()
			if err != nil {
				sm.logger.Error().Err(err).Msg("fail to reset the stream,skip it")
			}
		}
		sm.streamLocker.Lock()
		delete(sm.unusedStreams, msgID)
		delete(sm.unusedStreams, "UNKNOWN")
		sm.streamLocker.Unlock()
	}
}

func (sm *StreamMgr) AddStream(msgID string, stream network.Stream) {
	if stream == nil {
		return
	}
	sm.streamLocker.Lock()
	defer sm.streamLocker.Unlock()
	entries, ok := sm.unusedStreams[msgID]
	if !ok {
		entries := []network.Stream{stream}
		sm.unusedStreams[msgID] = entries
	} else {
		entries = append(entries, stream)
		sm.unusedStreams[msgID] = entries
	}
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
	lengthBytes := make([]byte, LengthHeader)
	n, err := io.ReadFull(streamReader, lengthBytes)
	if n != LengthHeader || err != nil {
		return nil, errors.Wrap(err, "error in read the message head")
	}
	length := binary.LittleEndian.Uint32(lengthBytes)
	if length > MaxPayload {
		return nil, errors.Errorf("payload length:%d exceed max payload length:%d", length, MaxPayload)
	}
	dataBuf := make([]byte, length)
	n, err = io.ReadFull(streamReader, dataBuf)
	if uint32(n) != length || err != nil {
		return nil, errors.Wrapf(
			err,
			"short read, we would like to read: %d, however we only read: %d",
			length,
			n,
		)
	}
	return dataBuf, nil
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
