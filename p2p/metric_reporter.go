package p2p

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/logs"
)

type ResourceMetricReporter struct {
	logger zerolog.Logger
}

func NewResourceMetricReporter(logger zerolog.Logger) *ResourceMetricReporter {
	return &ResourceMetricReporter{
		logger: logger.With().Str(logs.Component, "metrics").Logger(),
	}
}

// AllowConn is invoked when opening a connection is allowed
func (rmr *ResourceMetricReporter) AllowConn(_ network.Direction, _ bool) {}

// BlockConn is invoked when opening a connection is blocked
func (rmr *ResourceMetricReporter) BlockConn(dir network.Direction, useFD bool) {
	rmr.logger.Error().
		Str("conn.direction", dir.String()).
		Bool("conn.use_fd", useFD).
		Msg("Connection blocked")
}

// AllowStream is invoked when opening a stream is allowed
func (rmr *ResourceMetricReporter) AllowStream(_ peer.ID, _ network.Direction) {}

// BlockStream is invoked when opening a stream is blocked
func (rmr *ResourceMetricReporter) BlockStream(p peer.ID, dir network.Direction) {
	rmr.logger.Error().
		Stringer(logs.Peer, p).
		Str("conn.direction", dir.String()).
		Msg("Stream blocked")
}

// AllowPeer is invoked when attaching ac connection to a peer is allowed
func (rmr *ResourceMetricReporter) AllowPeer(_ peer.ID) {}

// BlockPeer is invoked when attaching a connection to a peer is blocked
func (rmr *ResourceMetricReporter) BlockPeer(p peer.ID) {
	rmr.logger.Error().Stringer(logs.Peer, p).Msg("Peer is blocked")
}

// AllowProtocol is invoked when setting the protocol for a stream is allowed
func (rmr *ResourceMetricReporter) AllowProtocol(_ protocol.ID) {}

// BlockProtocol is invoked when setting the protocol for a stream is blocked
func (rmr *ResourceMetricReporter) BlockProtocol(proto protocol.ID) {
	rmr.logger.Error().Msgf("Protocol is blocked: %s", proto)
}

// BlockProtocolPeer is invoked when setting the protocol for a stream is blocked at the per protocol peer scope
func (rmr *ResourceMetricReporter) BlockProtocolPeer(proto protocol.ID, p peer.ID) {
	rmr.logger.Error().Stringer(logs.Peer, p).Msgf("Protocol peer is blocked, protocol: %s", proto)
}

// AllowService is invoked when setting the protocol for a stream is allowed
func (rmr *ResourceMetricReporter) AllowService(_ string) {}

// BlockService is invoked when setting the protocol for a stream is blocked
func (rmr *ResourceMetricReporter) BlockService(svc string) {
	rmr.logger.Error().Msgf("Service %q is blocked", svc)
}

// BlockServicePeer is invoked when setting the service for a stream is blocked at the per service peer scope
func (rmr *ResourceMetricReporter) BlockServicePeer(svc string, p peer.ID) {
	rmr.logger.Error().Stringer(logs.Peer, p).Msgf("Service: %q from peer is blocked", svc)
}

// AllowMemory is invoked when a memory reservation is allowed
func (rmr *ResourceMetricReporter) AllowMemory(_ int) {}

// BlockMemory is invoked when a memory reservation is blocked
func (rmr *ResourceMetricReporter) BlockMemory(size int) {
	rmr.logger.Error().Msgf("Memory blocked, size: %d", size)
}
