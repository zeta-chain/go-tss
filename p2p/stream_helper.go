package p2p

import (
	"bufio"
	"encoding/binary"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	LengthHeader        = 4 // LengthHeader represent how many bytes we used as header
	TimeoutReadPayload  = time.Second * 10
	TimeoutWritePayload = time.Second * 10
	MaxPayload          = 20000000 // 20M
)

// applyDeadline will be true , and only disable it when we are doing test
// the reason being the p2p network , mocknet, mock stream doesn't support SetReadDeadline ,SetWriteDeadline feature
var ApplyDeadline = true

type StreamMgr struct {
	UnusedStreams           map[string][]network.Stream
	JoinPartyInboundStreams map[string]bool
	streamLocker            *sync.RWMutex
	logger                  zerolog.Logger
	NumStream               atomic.Int64
}

func NewStreamMgr() *StreamMgr {
	return &StreamMgr{
		UnusedStreams:           make(map[string][]network.Stream),
		JoinPartyInboundStreams: make(map[string]bool),
		streamLocker:            &sync.RWMutex{},
		logger:                  log.With().Str("module", "communication").Logger(),
	}
}

func (sm *StreamMgr) ReleaseStream(msgID string) {
	sm.streamLocker.Lock()
	usedStreams, okStream := sm.UnusedStreams[msgID]
	unknownStreams, okUnknown := sm.UnusedStreams["UNKNOWN"]
	if okStream {
		delete(sm.UnusedStreams, msgID)
	}
	if okUnknown {
		delete(sm.UnusedStreams, "UNKNOWN")
	}
	sm.streamLocker.Unlock()
	streams := append(usedStreams, unknownStreams...)
	cnt := int64(0)
	if okStream || okUnknown {
		for _, el := range streams {
			_, ok := sm.JoinPartyInboundStreams[el.ID()]
			if ok {
				delete(sm.JoinPartyInboundStreams, el.ID())
			}
			err := el.Reset()
			if err != nil {
				sm.logger.Error().Err(err).Msg("fail to reset the stream,skip it")
			}
			cnt++
		}
		sm.NumStream.Add(-cnt)
	}
	//sm.logger.Info().Msgf("release stream, msgID: %s, NumStream: %d, Unknown streams: %d, total: %d", msgID, len(streams), unknownStreams, sm.NumStream.Load())
}

func (sm *StreamMgr) AddStream(msgID string, stream network.Stream) {
	if stream == nil {
		return
	}
	sm.streamLocker.Lock()
	defer sm.streamLocker.Unlock()
	entries, ok := sm.UnusedStreams[msgID]
	if !ok {
		entries := []network.Stream{stream}
		sm.UnusedStreams[msgID] = entries
	} else {
		entries = append(entries, stream)
		sm.UnusedStreams[msgID] = entries
	}
	sm.NumStream.Add(1)
	//sm.logger.Info().Msgf("add stream, msgID: %s, NumStream: %d, total: %d", msgID, len(entries), sm.NumStream.Load())
}

func (sm *StreamMgr) AddInboundStream(stream network.Stream) {
	sm.streamLocker.Lock()
	defer sm.streamLocker.Unlock()
	sm.JoinPartyInboundStreams[stream.ID()] = true
}

// ReadStreamWithBuffer read data from the given stream
func ReadStreamWithBuffer(stream network.Stream) ([]byte, error) {
	if ApplyDeadline {
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
		return nil, fmt.Errorf("error in read the message head %w", err)
	}
	length := binary.LittleEndian.Uint32(lengthBytes)
	if length > MaxPayload {
		return nil, fmt.Errorf("payload length:%d exceed max payload length:%d", length, MaxPayload)
	}
	dataBuf := make([]byte, length)
	n, err = io.ReadFull(streamReader, dataBuf)
	if uint32(n) != length || err != nil {
		return nil, fmt.Errorf("short read err(%w), we would like to read: %d, however we only read: %d", err, length, n)
	}
	return dataBuf, nil
}

// WriteStreamWithBuffer write the message to stream
func WriteStreamWithBuffer(msg []byte, stream network.Stream) error {
	length := uint32(len(msg))
	lengthBytes := make([]byte, LengthHeader)
	binary.LittleEndian.PutUint32(lengthBytes, length)
	if ApplyDeadline {
		if err := stream.SetWriteDeadline(time.Now().Add(TimeoutWritePayload)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return errReset
			}
			return err
		}
	}
	streamWrite := bufio.NewWriter(stream)
	n, err := streamWrite.Write(lengthBytes)
	if n != LengthHeader || err != nil {
		return fmt.Errorf("fail to write head: %w", err)
	}
	n, err = streamWrite.Write(msg)
	if err != nil {
		return err
	}
	if uint32(n) != length {
		return fmt.Errorf("short write, we would like to write: %d, however we only write: %d", length, n)
	}
	err = streamWrite.Flush()
	if err != nil {
		return fmt.Errorf("fail to flush stream: %w", err)
	}
	return nil
}
