package p2p

import (
	"sort"
	"testing"

	tnet "github.com/libp2p/go-libp2p-testing/net"
	"github.com/libp2p/go-libp2p/core/peer"
	. "gopkg.in/check.v1"
)

// Hook up gocheck into the "go test" runner.
func Test(t *testing.T) { TestingT(t) }

type PeerStatusTestSuite struct {
}

var _ = Suite(&PeerStatusTestSuite{})

func generateRandomPeers(c *C, n int) []peer.ID {
	var peers []peer.ID
	for i := 0; i < n; i++ {
		node, err := tnet.RandIdentity()
		c.Assert(err, IsNil)
		peers = append(peers, node.ID())
	}
	return peers
}

func sortPeers(peers []peer.ID) {
	sort.Slice(peers, func(i, j int) bool {
		return peers[i].String() > peers[j].String()
	})
}

func (s *PeerStatusTestSuite) TestPeerStatus(c *C) {
	peers := generateRandomPeers(c, 5)
	sortPeers(peers)

	peerStatus := newPeerStatus(peers, peers[0], peers[0], 2)

	ret, err := peerStatus.updatePeer(peers[2])
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, false)
	ret, err = peerStatus.updatePeer(peers[1])
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, true)
	ret, err = peerStatus.updatePeer(peers[2])
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, false)
	online, offline := peerStatus.getPeersStatus()
	sortPeers(online)
	sortPeers(offline)

	c.Assert(online, DeepEquals, peers[1:3])
	c.Assert(offline, DeepEquals, peers[3:])
	ret = peerStatus.getCoordinationStatus()
	c.Assert(ret, Equals, false)

	unknownPeer := generateRandomPeers(c, 1)
	_, err = peerStatus.updatePeer(unknownPeer[0])
	c.Assert(err, ErrorMatches, "key not found")

	ret, err = peerStatus.updatePeer(peers[3])
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, false)
	ret, err = peerStatus.updatePeer(peers[4])
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, false)
}
