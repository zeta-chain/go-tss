package p2p

import (
	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/rs/zerolog"

	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
)

type WhitelistConnectionGater struct {
	whitelistedPeers map[peer.ID]bool
	logger           zerolog.Logger
}

func (wg WhitelistConnectionGater) InterceptPeerDial(p peer.ID) (allow bool) {
	wg.logger.Info().Msgf("InterceptPeerDial %s", p.String())
	// _, allow = wg.whitelistedPeers[p]
	return true
}

func (wg WhitelistConnectionGater) InterceptAddrDial(p peer.ID, m maddr.Multiaddr) (allow bool) {
	wg.logger.Info().Msgf("InterceptAddrDial %s", p.String())
	// Not checking addresses here, just allowing based on peer ID
	return true
}

func (wg WhitelistConnectionGater) InterceptAccept(network.ConnMultiaddrs) (allow bool) {
	return true
}

func (wg WhitelistConnectionGater) InterceptSecured(direction network.Direction, p peer.ID, _ network.ConnMultiaddrs) (allow bool) {
	_, allow = wg.whitelistedPeers[p]
	return allow
}

func (wg WhitelistConnectionGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	// Allow connection upgrades
	return true, 0
}
