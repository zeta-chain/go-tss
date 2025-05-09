// Package config contains the constants for the TSS library
package config

import (
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/protocol"
	resources "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

const (
	// StreamTimeoutConnect allow this duration to connect to the peer
	StreamTimeoutConnect = 10 * time.Second

	// StreamTimeoutRead allow this duration to read from the stream
	StreamTimeoutRead = 20 * time.Second

	// StreamTimeoutWrite allow this duration to write to the stream
	StreamTimeoutWrite = 20 * time.Second

	// StreamExcessTTL is the cleanup interval && TTL for "waste" streams (aka streams that are not used)
	StreamExcessTTL = 1 * time.Minute

	// StreamMaxPayload is the max payload for a stream in bytes. 20MB
	StreamMaxPayload = 20 << 20
)

// Signature Notifier related constants
const (
	// SigNotifierAckTimeout is the duration to receive ACK back from the peer
	SigNotifierAckTimeout = 2 * time.Second

	// SigNotifierTTL is the TTL for the notifier. Will be cleaned up if no response from the peer
	SigNotifierTTL             = 30 * time.Second
	SigNotifierCleanupInterval = 15 * time.Second
)

// TSSCommonFinalTimeout is the graceful timeout for keygen/keysign finalization
const TSSCommonFinalTimeout = 5 * time.Second

// PartyJoinMemberRetryInterval retry interval for joining keygen/keysign party
const PartyJoinMemberRetryInterval = 500 * time.Millisecond

// ScalingLimits creates a config for libp2p scaling limits
func ScalingLimits(protocols ...protocol.ID) resources.ConcreteLimitConfig {
	limits := resources.DefaultLimits

	base := resources.BaseLimit{
		Streams:         4096,
		StreamsInbound:  2048,
		StreamsOutbound: 2048,
		Memory:          mb(512),
	}

	increase := resources.BaseLimitIncrease{
		Streams:         512,
		StreamsInbound:  256,
		StreamsOutbound: 256,
		Memory:          mb(64),
	}

	limits.ProtocolBaseLimit = base
	limits.ProtocolLimitIncrease = increase

	for _, protocol := range protocols {
		limits.AddProtocolLimit(protocol, base, increase)
		limits.AddProtocolPeerLimit(protocol, base, increase)
	}

	// Add limits around included libp2p protocols
	libp2p.SetDefaultServiceLimits(&limits)

	// Turn the scaling limits into a static set of limits using `.AutoScale`. This
	// scales the limits proportional to system's memory.
	return limits.AutoScale()
}

// mb converts a number of megabytes to bytes
func mb(n int64) int64 {
	return n << 20
}
