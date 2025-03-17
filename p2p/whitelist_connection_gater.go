package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
)

type WhitelistConnectionGater struct {
	whitelistedPeers map[peer.ID]bool
	logger           zerolog.Logger
}

func NewWhitelistConnectionGater(whitelistedPeers []peer.ID, logger zerolog.Logger) *WhitelistConnectionGater {
	gater := &WhitelistConnectionGater{
		logger:           logger,
		whitelistedPeers: make(map[peer.ID]bool),
	}

	for _, p := range whitelistedPeers {
		logger.Info().
			Stringer("peer", p).
			Msg("Adding peer to whitelist")
		gater.whitelistedPeers[p] = true
	}

	return gater
}

func (wg *WhitelistConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	return wg.peerAllowed("InterceptPeerDial", p, nil)
}

func (wg *WhitelistConnectionGater) InterceptAddrDial(p peer.ID, m maddr.Multiaddr) (allow bool) {
	return wg.peerAllowed("InterceptAddrDial", p, &m)
}

func (wg *WhitelistConnectionGater) InterceptAccept(_ network.ConnMultiaddrs) (allow bool) {
	return true
}

func (wg *WhitelistConnectionGater) InterceptSecured(
	_ network.Direction,
	p peer.ID,
	m network.ConnMultiaddrs,
) (allow bool) {
	remoteMultiAddr := m.RemoteMultiaddr()
	return wg.peerAllowed("InterceptSecured", p, &remoteMultiAddr)
}

func (wg *WhitelistConnectionGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	// Allow connection upgrades
	return true, 0
}

func (wg *WhitelistConnectionGater) peerAllowed(interceptor string, p peer.ID, remoteAddr *maddr.Multiaddr) bool {
	allowed := wg.whitelistedPeers[p]

	var event *zerolog.Event
	if allowed {
		event = wg.logger.Debug() // log allowed peers at Debug level
	} else {
		event = wg.logger.Info() // log denied peers at Info level
	}

	event = event.
		Str("interceptor", interceptor).
		Stringer("peer", p).
		Bool("allowed", allowed)

	if remoteAddr != nil {
		event.Str("remote_address", (*remoteAddr).String())
	}

	if allowed {
		event.Msg("Peer allowed")
	} else {
		event.Msg("Peer denied")
	}

	return allowed
}
