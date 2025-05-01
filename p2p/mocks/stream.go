package mocks

import (
	"bytes"
	"math/rand/v2"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

const testProtocolID protocol.ID = "/p2p/test-stream"

type Stream struct {
	t      *testing.T
	buffer *bytes.Buffer

	id       uint64
	protocol protocol.ID

	readDeadline  time.Time
	writeDeadline time.Time

	errSetReadDeadLine  bool
	errSetWriteDeadLine bool
	errRead             bool

	mu *sync.RWMutex
}

var _ network.Stream = &Stream{}

func NewStream(t *testing.T) *Stream {
	return &Stream{
		t:      t,
		buffer: &bytes.Buffer{},

		id:       rand.Uint64() % 10_000,
		protocol: testProtocolID,

		readDeadline:  time.Time{},
		writeDeadline: time.Time{},

		errSetReadDeadLine:  false,
		errSetWriteDeadLine: false,
		errRead:             false,

		mu: &sync.RWMutex{},
	}
}

func (s *Stream) Read(buf []byte) (n int, err error) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.errRead {
		return 0, errors.New("you asked for it")
	}

	// no deadline, read immediately (sync)
	if s.readDeadline.IsZero() {
		return s.buffer.Read(buf)
	}

	var (
		timeout = time.Until(s.readDeadline)
		done    = make(chan struct{})
	)

	go func() {
		n, err = s.buffer.Read(buf)
		close(done)
	}()

	select {
	case <-done:
		return n, err
	case <-time.After(timeout):
		return 0, errors.New("mock: read deadline exceeded")
	}
}

func (s *Stream) MustRead(buf []byte) {
	_, err := s.Read(buf)
	require.NoError(s.t, err, "failed to read from stream")
}

func (s *Stream) Write(buf []byte) (n int, err error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.errSetWriteDeadLine {
		return 0, errors.New("mock: unable to set write deadline")
	}

	// no deadline, write immediately (sync)
	if s.writeDeadline.IsZero() {
		return s.buffer.Write(buf)
	}

	var (
		timeout = time.Until(s.writeDeadline)
		done    = make(chan struct{})
	)

	go func() {
		n, err = s.buffer.Write(buf)
		close(done)
	}()

	select {
	case <-done:
		return n, err
	case <-time.After(timeout):
		return 0, errors.New("mock: write deadline exceeded")
	}
}

func (s *Stream) MustWrite(buf []byte) {
	_, err := s.Write(buf)
	require.NoError(s.t, err, "failed to write to stream")
}

func (s *Stream) Stat() network.Stats {
	return network.Stats{
		Direction: network.DirUnknown,
		Extra:     make(map[any]any),
	}
}

func (s *Stream) Protocol() protocol.ID {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.protocol
}

func (s *Stream) SetProtocol(id protocol.ID) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.protocol = id

	return nil
}

func (s *Stream) SetDeadline(at time.Time) error {
	if err := s.SetReadDeadline(at); err != nil {
		return err
	}

	if err := s.SetWriteDeadline(at); err != nil {
		return err
	}

	return nil
}

func (s *Stream) SetReadDeadline(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.errSetReadDeadLine {
		return errors.New("mock: unable to set read deadline")
	}

	s.readDeadline = at

	return nil
}

func (s *Stream) SetWriteDeadline(at time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.errSetWriteDeadLine {
		return errors.New("mock: unable to set write deadline")
	}

	s.writeDeadline = at

	return nil
}

func (s *Stream) ErrRead(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.errRead = v
}

func (s *Stream) ErrSetReadDeadline(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.errSetReadDeadLine = v
}

func (s *Stream) ErrSetWriteDeadline(v bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.errSetWriteDeadLine = v
}

func (s *Stream) ID() string                 { return strconv.FormatUint(s.id, 10) }
func (s *Stream) Scope() network.StreamScope { return &network.NullScope{} }
func (s *Stream) Conn() network.Conn         { return nil }
func (s *Stream) Close() error               { return nil }
func (s *Stream) CloseRead() error           { return nil }
func (s *Stream) CloseWrite() error          { return nil }
func (s *Stream) Reset() error               { return nil }
