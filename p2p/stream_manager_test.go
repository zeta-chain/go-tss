package p2p

import (
	"bytes"
	"encoding/binary"
	"strconv"
	"testing"
	"time"

	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
)

const testProtocolID protocol.ID = "/p2p/test-stream"

type MockNetworkStream struct {
	*bytes.Buffer
	protocol            protocol.ID
	errSetReadDeadLine  bool
	errSetWriteDeadLine bool
	errRead             bool
	id                  int64
}

func NewMockNetworkStream() *MockNetworkStream {
	return &MockNetworkStream{
		Buffer:   &bytes.Buffer{},
		protocol: testProtocolID,
	}
}

func (m MockNetworkStream) Read(buf []byte) (int, error) {
	if m.errRead {
		return 0, errors.New("you asked for it")
	}
	return m.Buffer.Read(buf)
}

func (m MockNetworkStream) Close() error {
	return nil
}

func (m MockNetworkStream) CloseRead() error {
	return nil
}

func (m MockNetworkStream) CloseWrite() error {
	return nil
}

func (m MockNetworkStream) Reset() error {
	return nil
}

func (m MockNetworkStream) SetDeadline(time.Time) error {
	return nil
}

func (m MockNetworkStream) SetReadDeadline(time.Time) error {
	if m.errSetReadDeadLine {
		return errors.New("you asked for it")
	}
	return nil
}

func (m MockNetworkStream) SetWriteDeadline(time.Time) error {
	if m.errSetWriteDeadLine {
		return errors.New("you asked for it")
	}
	return nil
}

func (m MockNetworkStream) Protocol() protocol.ID {
	return m.protocol
}

func (m MockNetworkStream) SetProtocol(id protocol.ID) error {
	m.protocol = id
	return nil
}

func (s MockNetworkStream) ID() string {
	return strconv.FormatInt(s.id, 10)
}

func (m MockNetworkStream) Stat() network.Stats {
	return network.Stats{
		Direction: 0,
		Extra:     make(map[interface{}]interface{}),
	}
}

func (m MockNetworkStream) Conn() network.Conn {
	return nil
}

func (m MockNetworkStream) Scope() network.StreamScope {
	return &network.NullScope{}
}

func TestReadLength(t *testing.T) {
	testCases := []struct {
		name           string
		streamProvider func() network.Stream
		expectedLength uint32
		expectError    bool
		validator      func(t *testing.T)
	}{
		{
			name:           "happy path",
			expectedLength: 1024,
			expectError:    false,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1024))
				return s
			},
		},
		{
			name:           "fail to set read dead line should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				s.errSetReadDeadLine = true
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1024))
				return s
			},
		},
		{
			name:           "read exactly the given length of data",
			expectedLength: 1024,
			expectError:    false,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.Buffer.Write(bytes.Repeat([]byte("a"), 1026))
				return s
			},
		},
		{
			name:           "fail to read should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := NewMockNetworkStream()
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)
				s.Buffer.Write(buf)
				s.errRead = true
				return s
			},
		},
	}
	for _, tc := range testCases {
		ApplyDeadline.Store(true)
		t.Run(tc.name, func(st *testing.T) {
			stream := tc.streamProvider()
			l, err := ReadStreamWithBuffer(stream)
			if tc.expectError && err == nil {
				st.Errorf("expecting error , however got none")
				st.FailNow()
			}
			if !tc.expectError && err != nil {
				st.Error(err)
				st.FailNow()
			}
			if !tc.expectError && tc.expectedLength != uint32(len(l)) {
				st.Errorf("expecting length to be %d, however got :%d", tc.expectedLength, l)
				st.FailNow()
			}
		})
	}
}

func TestReadPayload(t *testing.T) {
	testCases := []struct {
		name           string
		streamProvider func() *MockNetworkStream
		expectedBytes  []byte
		expectError    bool
	}{
		{
			name: "happy path",
			streamProvider: func() *MockNetworkStream {
				stream := NewMockNetworkStream()
				input := []byte("hello world")
				err := WriteStreamWithBuffer(input, stream)
				if err != nil {
					t.Errorf("fail to write the data to stream")
					t.FailNow()
				}
				return stream
			},
			expectedBytes: []byte("hello world"),
			expectError:   false,
		},
	}
	for _, tc := range testCases {
		ApplyDeadline.Store(true)
		t.Run(tc.name, func(st *testing.T) {
			stream := tc.streamProvider()
			l, err := ReadStreamWithBuffer(stream)
			if err != nil {
				st.Errorf("fail to read length:%s", err)
				st.FailNow()
			}
			if tc.expectError && err == nil {
				st.Errorf("expecting error , however got none")
				st.FailNow()
			}
			if !tc.expectError && err != nil {
				st.Error(err)
				st.FailNow()
			}

			if !tc.expectError && !bytes.Equal(tc.expectedBytes, l) {
				st.Errorf("expecting %s, however got :%s", string(tc.expectedBytes), string(l))
				st.FailNow()
			}
		})
	}
}

func TestStreamManager(t *testing.T) {
	// ARRANGE
	streamManager := NewStreamManager(zerolog.New(zerolog.NewTestWriter(t)))

	streamA := NewMockNetworkStream()
	streamA.id = 10

	streamB := NewMockNetworkStream()
	streamB.id = 20

	streamC := NewMockNetworkStream()
	streamC.id = 30

	streamD := NewMockNetworkStream()
	streamD.id = 40

	// ACT

	// noop
	streamManager.Stash("1", nil)
	require.Equal(t, 0, len(streamManager.streams))

	streamManager.Stash("1", streamA)
	streamManager.Stash("2", streamB)
	streamManager.Stash("3", streamC)
	streamManager.Stash("3", streamD)

	streamManager.Free("1")

	_, ok := streamManager.streams["10"]
	assert.False(t, ok)

	si, ok := streamManager.streams["20"]
	assert.True(t, ok)
	assert.Equal(t, "2", si.msgID)

	_, ok = streamManager.streams["30"]
	assert.True(t, ok)

	streamManager.Free("2")
	_, ok = streamManager.streams["20"]
	assert.False(t, ok)

	streamManager.Free("3")
	assert.Equal(t, 0, len(streamManager.streams))

	streamManager.Free("3")
	assert.Equal(t, 0, len(streamManager.streams))
}
