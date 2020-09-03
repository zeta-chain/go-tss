package p2p

import (
	"testing"

	. "gopkg.in/check.v1"
)

func TestPackage(t *testing.T) { TestingT(t) }

type LeaderProviderTestSuite struct{}

var _ = Suite(&LeaderProviderTestSuite{})

func (t *LeaderProviderTestSuite) TestLeaderNode(c *C) {
	testPeers := []string{
		"16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp", "16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh",
		"16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa",
	}
	ret, err := LeaderNode("HelloWorld", 10, testPeers)
	c.Assert(err, IsNil)
	c.Assert(ret, Equals, testPeers[1])
}
