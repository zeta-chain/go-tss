package p2p

import (
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

type ResourceMetricReporter struct {
	logger zerolog.Logger
}

func NewResourceMetricReporter() *ResourceMetricReporter {
	return &ResourceMetricReporter{
		logger: log.With().Str("module", "p2p_resource").Logger(),
	}
}

// AllowConn is invoked when opening a connection is allowed
func (rmr *ResourceMetricReporter) AllowConn(dir network.Direction, usefd bool) {

}

// BlockConn is invoked when opening a connection is blocked
func (rmr *ResourceMetricReporter) BlockConn(dir network.Direction, usefd bool) {
	rmr.logger.Error().Msgf("connection blocked, direction: %s, usefd: %+v", dir.String(), usefd)
}

// AllowStream is invoked when opening a stream is allowed
func (rmr *ResourceMetricReporter) AllowStream(p peer.ID, dir network.Direction) {

}

// BlockStream is invoked when opening a stream is blocked
func (rmr *ResourceMetricReporter) BlockStream(p peer.ID, dir network.Direction) {
	rmr.logger.Error().Msgf("stream blocked,peer: %s, dir: %s", p, dir)
}

// AllowPeer is invoked when attaching ac onnection to a peer is allowed
func (rmr *ResourceMetricReporter) AllowPeer(p peer.ID) {}

// BlockPeer is invoked when attaching a connection to a peer is blocked
func (rmr *ResourceMetricReporter) BlockPeer(p peer.ID) {
	rmr.logger.Error().Msgf("peer: %s is blocked", p)
}

// AllowProtocol is invoked when setting the protocol for a stream is allowed
func (rmr *ResourceMetricReporter) AllowProtocol(proto protocol.ID) {}

// BlockProtocol is invoked when setting the protocol for a stream is blocked
func (rmr *ResourceMetricReporter) BlockProtocol(proto protocol.ID) {
	rmr.logger.Error().Msgf("protocol is blocked: %s", proto)
}

// BlockProtocolPeer is invoked when setting the protocol for a stream is blocked at the per protocol peer scope
func (rmr *ResourceMetricReporter) BlockProtocolPeer(proto protocol.ID, p peer.ID) {
	rmr.logger.Error().Msgf("protocol peer is blocked, protocol: %s, peer: %s", proto, p)
}

// AllowService is invoked when setting the protocol for a stream is allowed
func (rmr *ResourceMetricReporter) AllowService(svc string) {}

// BlockService is invoked when setting the protocol for a stream is blocked
func (rmr *ResourceMetricReporter) BlockService(svc string) {
	rmr.logger.Error().Msgf("service:%s is blocked", svc)
}

// BlockServicePeer is invoked when setting the service for a stream is blocked at the per service peer scope
func (rmr *ResourceMetricReporter) BlockServicePeer(svc string, p peer.ID) {
	rmr.logger.Error().Msgf("service: %s from peer: %s is blocked", svc, p)
}

// AllowMemory is invoked when a memory reservation is allowed
func (rmr *ResourceMetricReporter) AllowMemory(size int) {}

// BlockMemory is invoked when a memory reservation is blocked
func (rmr *ResourceMetricReporter) BlockMemory(size int) {
	rmr.logger.Error().Msgf("memory blocked , size: %d", size)
}
