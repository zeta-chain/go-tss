package p2p

import (
	"fmt"
	"strings"

	"github.com/libp2p/go-libp2p/core/control"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/rs/zerolog"

	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
)

type WhitelistConnectionGater struct {
	whitelistedPeers map[string]bool
	logger           zerolog.Logger
	disableWhitelist bool
}

func NewWhitelistConnectionGater(whitelistedPeers []string, disableWhitelist bool, logger zerolog.Logger) *WhitelistConnectionGater {
	gater := &WhitelistConnectionGater{
		disableWhitelist: disableWhitelist,
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
	wg.logger.Info().Msgf("InterceptPeerDial %s", p.String())
	if !wg.disableWhitelist {
		wg.logger.Info().Msgf("peer allowed %t", wg.whitelistedPeers[p.String()])
		return wg.whitelistedPeers[p.String()]
	}
	return true
}

func (wg *WhitelistConnectionGater) InterceptAddrDial(p peer.ID, m maddr.Multiaddr) (allow bool) {
	wg.logger.Info().Msgf("InterceptAddrDial %s %s", p.String(), m.String())
	if !wg.disableWhitelist {
		wg.logger.Info().Msgf("peer allowed %t", wg.whitelistedPeers[p.String()])
		return wg.whitelistedPeers[p.String()]
	}
	// Not checking addresses here, just allowing based on peer ID
	return true
}

func (wg *WhitelistConnectionGater) InterceptAccept(m network.ConnMultiaddrs) (allow bool) {
	hasAllowedPort := hasAllowedPort(m, "6668")
	wg.logger.Info().Msgf("InterceptAccept %t", hasAllowedPort)

	return true
}

func (wg *WhitelistConnectionGater) InterceptSecured(direction network.Direction, p peer.ID, m network.ConnMultiaddrs) (allow bool) {
	hasAllowedPort := hasAllowedPort(m, "6668")
	wg.logger.Info().Msgf("InterceptSecured %t %s", hasAllowedPort, p.String())
	if !wg.disableWhitelist {
		wg.logger.Info().Msgf("peer allowed %t", wg.whitelistedPeers[p.String()])
		return wg.whitelistedPeers[p.String()]
	}
	// _, allow = wg.whitelistedPeers[p]
	return true
}

func (wg *WhitelistConnectionGater) InterceptUpgraded(network.Conn) (bool, control.DisconnectReason) {
	// Allow connection upgrades
	return true, 0
}

// Helper function to check if multiaddr has the correct TCP port
func hasAllowedPort(multiaddrs network.ConnMultiaddrs, allowedPort string) bool {
	fmt.Println("multiaddrs", multiaddrs.RemoteMultiaddr().String(), multiaddrs.RemoteMultiaddr().Protocols())
	return strings.Contains(multiaddrs.RemoteMultiaddr().String(), "/tcp/"+allowedPort)
}
