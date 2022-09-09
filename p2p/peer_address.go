package p2p

import (

	"github.com/libp2p/go-libp2p/core/peer"
)

func (c *Communication) ExportPeerAddress() map[peer.ID]AddrList {
	peerStore := c.host.Peerstore()
	peers := peerStore.Peers()
	addressBook := make(map[peer.ID]AddrList)
	for _, el := range peers {
		addrs := peerStore.Addrs(el)
		addressBook[el] = addrs
	}
	return addressBook
}
