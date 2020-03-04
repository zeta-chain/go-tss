package p2p

import (
	"github.com/libp2p/go-libp2p-core/peer"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

type JoinPartyTestSuite struct{}

var _ = Suite(&JoinPartyTestSuite{})

func (JoinPartyTestSuite) TestNewJoinParty(c *C) {
	req := &messages.JoinPartyRequest{
		ID: "whatever",
	}
	id, err := peer.Decode("16Uiu2HAm1PcCAcUZd6N4RZWnbmBHjb14Hm5iE98BY6xi7R4otHCP")
	c.Assert(err, IsNil)
	joinParty := NewJoinParty(req, id)
	c.Assert(joinParty, NotNil)
}
