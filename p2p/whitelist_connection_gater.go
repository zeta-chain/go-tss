package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/rs/zerolog"

	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
)

type WhitelistConnectionGater struct {
	whitelistedPeers map[string]bool
	logger           zerolog.Logger
}

func NewWhitelistConnectionGater(whitelistedPeers []string, logger zerolog.Logger) *WhitelistConnectionGater {
	gater := &WhitelistConnectionGater{
		logger:           logger,
		whitelistedPeers: make(map[string]bool),
	}

	for _, p := range whitelistedPeers {
		logger.Info().Msgf("Adding peer %s to whitelist", p)
		gater.whitelistedPeers[p] = true
	}

	return gater
}

func (wg *WhitelistConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	return wg.peerAllowed("InterceptPeerDial", p.String())
}

func (wg *WhitelistConnectionGater) InterceptAddrDial(p peer.ID, m maddr.Multiaddr) (allow bool) {
	return wg.peerAllowed("InterceptAddrDial", p.String())
}

func (wg *WhitelistConnectionGater) InterceptAccept(m network.ConnMultiaddrs) (allow bool) {
	return true
}

func (wg *WhitelistConnectionGater) InterceptSecured(direction network.Direction, p peer.ID, m network.ConnMultiaddrs) (allow bool) {
	return wg.peerAllowed("InterceptSecured", p.String())
}

func (wg *WhitelistConnectionGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	// Allow connection upgrades
	return true, 0
}

func (wg *WhitelistConnectionGater) peerAllowed(p string, interceptor string) bool {
	allowed := wg.whitelistedPeers[p]

	if allowed {
		// TODO: switch to debug
		wg.logger.Info().Msgf("%s: peer %s allowed", interceptor, p)
	} else {
		wg.logger.Info().Msgf("%s: peer %s denied", interceptor, p)
	}

	return allowed
}
