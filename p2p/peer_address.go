package p2p

import (
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
)

func (c *Communication) ExportPeerAddress() map[peer.ID][]maddr.Multiaddr {
	peerStore := c.host.Peerstore()
	peers := peerStore.Peers()
	addressBook := make(map[peer.ID][]maddr.Multiaddr)
	for _, el := range peers {
		addrs := peerStore.Addrs(el)
		addressBook[el] = addrs
	}
	return addressBook
}
