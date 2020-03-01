package p2p

import (
	"testing"

	. "gopkg.in/check.v1"
)

func TestPackage(t *testing.T) { TestingT(t) }

type LeaderProviderTestSuite struct{}

var _ = Suite(&LeaderProviderTestSuite{})

func (t *LeaderProviderTestSuite) TestLeaderNode(c *C) {
	idx, err := LeaderNode([]byte("HelloWorld"), 5)
	c.Assert(err, IsNil)
	c.Assert(idx >= 0, Equals, true)
	c.Assert(idx, Equals, int32(1))
}
