package p2p

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/libp2p/go-libp2p-core/network"
)

const (
	LengthHeader       = 4 // LengthHeader represent how many bytes we used as header
	TimeoutReadHeader  = time.Second
	TimeoutReadPayload = time.Second * 2
	TimeoutWriteHeader = time.Second
)

// applyDeadline will be true , and only disable it when we are doing test
// the reason being the p2p network , mocknet, mock stream doesn't support SetReadDeadline ,SetWriteDeadline feature
var applyDeadline = true

// ReadLength will read the length from stream
func ReadLength(stream network.Stream) (uint32, error) {
	buf := make([]byte, LengthHeader)
	r := io.LimitReader(stream, LengthHeader)
	if applyDeadline {
		if err := stream.SetReadDeadline(time.Now().Add(TimeoutReadHeader)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return 0, errReset
			}
			return 0, err
		}
	}
	_, err := r.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		if errReset := stream.Reset(); errReset != nil {
			return 0, errReset
		}
		return 0, err
	}
	return binary.LittleEndian.Uint32(buf), nil
}

// ReadPayload from stream
func ReadPayload(stream network.Stream, length uint32) ([]byte, error) {
	buf := make([]byte, length)
	if applyDeadline {
		if err := stream.SetReadDeadline(time.Now().Add(TimeoutReadPayload)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return nil, errReset
			}
			return nil, err
		}
	}

	_, err := stream.Read(buf)
	if err != nil && !errors.Is(err, io.EOF) {
		if errReset := stream.Reset(); errReset != nil {
			return nil, errReset
		}
		return nil, err
	}
	return buf, nil
}

// WriteLength write the given length as header
func WriteLength(stream network.Stream, length uint32) error {
	buf := make([]byte, LengthHeader)
	binary.LittleEndian.PutUint32(buf, length)
	if applyDeadline {
		if err := stream.SetWriteDeadline(time.Now().Add(TimeoutWriteHeader)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return errReset
			}
			return fmt.Errorf("fail to write length to stream: %w", err)
		}
	}
	_, err := stream.Write(buf)
	if err != nil {
		if errReset := stream.Reset(); errReset != nil {
			return errReset
		}
		return fmt.Errorf("fail to write to peer(%s): %w", stream.Conn().RemotePeer().String(), err)
	}
	return nil
}
