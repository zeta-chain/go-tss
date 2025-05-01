package p2p

import (
	"bytes"
	"encoding/binary"
	"testing"

	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeta-chain/go-tss/p2p/mocks"

	"github.com/libp2p/go-libp2p/core/network"
)

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
				s := mocks.NewStream(t)
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)

				s.MustWrite(buf)
				s.MustWrite(bytes.Repeat([]byte("a"), 1024))

				return s
			},
		},
		{
			name:           "fail to set read dead line should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := mocks.NewStream(t)
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)

				s.MustWrite(buf)
				s.MustWrite(bytes.Repeat([]byte("a"), 1024))

				s.ErrSetReadDeadline(true)

				return s
			},
		},
		{
			name:           "read exactly the given length of data",
			expectedLength: 1024,
			expectError:    false,
			streamProvider: func() network.Stream {
				s := mocks.NewStream(t)
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)

				s.MustWrite(buf)
				s.MustWrite(bytes.Repeat([]byte("a"), 1026))

				return s
			},
		},
		{
			name:           "fail to read should return an error",
			expectedLength: 1024,
			expectError:    true,
			streamProvider: func() network.Stream {
				s := mocks.NewStream(t)
				buf := make([]byte, PayloadHeaderLen)
				binary.LittleEndian.PutUint32(buf, 1024)

				s.MustWrite(buf)
				s.ErrRead(true)

				return s
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(st *testing.T) {
			// ARRANGE
			stream := tc.streamProvider()

			// ACT
			l, err := ReadStreamWithBuffer(stream)

			// ASSERT
			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err, "fail to read")
			require.Equal(t, tc.expectedLength, uint32(len(l)), "length mismatch")
		})
	}
}

func TestReadPayload(t *testing.T) {
	testCases := []struct {
		name           string
		streamProvider func() *mocks.Stream
		expectedBytes  []byte
		expectError    bool
	}{
		{
			name: "happy path",
			streamProvider: func() *mocks.Stream {
				stream := mocks.NewStream(t)
				input := []byte("hello world")

				err := WriteStreamWithBuffer(input, stream)
				require.NoError(t, err)

				return stream
			},
			expectedBytes: []byte("hello world"),
			expectError:   false,
		},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// ARRANGE
			stream := tc.streamProvider()

			// ACT
			l, err := ReadStreamWithBuffer(stream)

			// ASSERT
			if tc.expectError {
				require.Error(t, err)
				return
			}

			require.NoError(t, err, "fail to read")
			require.Equal(t, tc.expectedBytes, l, "bytes mismatch")
		})
	}
}

func TestStreamManager(t *testing.T) {
	// ARRANGE
	streamManager := NewStreamManager(zerolog.New(zerolog.NewTestWriter(t)))

	streamA := mocks.NewStream(t)
	streamB := mocks.NewStream(t)
	streamC := mocks.NewStream(t)
	streamD := mocks.NewStream(t)

	hasStream := func(s network.Stream) bool {
		_, ok := streamManager.streams[s.ID()]
		return ok
	}

	// ACT

	// noop
	streamManager.Stash("1", nil)
	require.Equal(t, 0, len(streamManager.streams))

	streamManager.Stash("1", streamA)
	streamManager.Stash("2", streamB)
	streamManager.Stash("3", streamC)
	streamManager.Stash("3", streamD)

	streamManager.Free("1")

	assert.False(t, hasStream(streamA))

	si, ok := streamManager.streams[streamB.ID()]
	assert.True(t, ok)
	assert.Equal(t, "2", si.msgID)

	assert.True(t, hasStream(streamC))

	streamManager.Free("2")
	assert.False(t, hasStream(streamB))

	streamManager.Free("3")
	assert.Equal(t, 0, len(streamManager.streams))

	streamManager.Free("3")
	assert.Equal(t, 0, len(streamManager.streams))
}
