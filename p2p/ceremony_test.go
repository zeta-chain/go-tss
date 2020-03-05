package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	. "gopkg.in/check.v1"

	go_tss "gitlab.com/thorchain/tss/go-tss"
)

type CeremonyTestSuite struct{}

var _ = Suite(&CeremonyTestSuite{})

func (CeremonyTestSuite) TestCeremony(c *C) {
	p1, p2, p3 := go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID()
	ce := &Ceremony{
		ID:                go_tss.RandStringBytesMask(24),
		Threshold:         3,
		JoinPartyRequests: nil,
		Status:            GatheringParties,
		Peers:             []peer.ID{p1, p2, p3},
	}
	c.Assert(ce.IsReady(), Equals, false)
	c.Assert(ce.ValidPeer(p1), Equals, true)
	c.Assert(ce.GetParties(), HasLen, 0)
	p4 := go_tss.GetRandomPeerID()
	c.Assert(ce.ValidPeer(p4), Equals, false)
}
