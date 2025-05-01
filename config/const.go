// Package config contains the constants for the TSS library
package config

import (
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/protocol"
	resources "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
)

// stream-related constants
const (
	StreamTimeoutConnect = 20 * time.Second
	StreamTimeoutRead    = 20 * time.Second
	StreamTimeoutWrite   = 20 * time.Second
	StreamMaxPayload     = 1024 * 1024 * 10 // 20MB
)

const StreamManagerMaxAgeBeforeCleanup = time.Minute

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
