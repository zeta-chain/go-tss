package p2p

import (
	"bufio"
	"encoding/binary"
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
var ApplyDeadline = true

func ReadStreamWithBuffer(stream network.Stream) ([]byte, error) {
	if ApplyDeadline {
		if err := stream.SetReadDeadline(time.Now().Add(TimeoutWriteHeader)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return nil, errReset
			}
			return nil, err
		}
	}
	streamReader := bufio.NewReader(stream)
	lengthBytes := make([]byte, HeadLength)
	n, err := io.ReadFull(streamReader, lengthBytes)
	if n != HeadLength || err != nil {
		retErr := fmt.Errorf("error in read the message head %w", err)
		return nil, retErr
	}
	length := binary.LittleEndian.Uint32(lengthBytes)
	dataBuf := make([]byte, length)
	n, err = io.ReadFull(streamReader, dataBuf)
	if uint32(n) != length || err != nil {
		retErr := fmt.Errorf("short read err(%w), we would like to read: %d, however we only read: %d", err, length, n)
		return nil, retErr
	}
	return dataBuf, nil
}

func WriteStreamWithBuffer(msg []byte, stream network.Stream) error {
	length := uint32(len(msg))
	lengthBytes := make([]byte, HeadLength)
	binary.LittleEndian.PutUint32(lengthBytes, length)
	if ApplyDeadline {
		if err := stream.SetWriteDeadline(time.Now().Add(TimeoutWriteHeader)); nil != err {
			if errReset := stream.Reset(); errReset != nil {
				return errReset
			}
			return err
		}
	}
	streamWrite := bufio.NewWriter(stream)
	n, err := streamWrite.Write(lengthBytes)
	if n != HeadLength || err != nil {
		return fmt.Errorf("fail to write head: %w", err)
	}
	n, err = streamWrite.Write(msg)
	if err != nil {
		return err
	}
	err = streamWrite.Flush()
	if uint32(n) != length || err != nil {
		return fmt.Errorf("short write, we would like to write: %d, however we only write: %d", length, n)
	}
	return nil
}
