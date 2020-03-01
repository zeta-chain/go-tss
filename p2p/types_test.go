package p2p

import (
	. "gopkg.in/check.v1"
)

type AddrListTestSuite struct{}

var _ = Suite(&AddrListTestSuite{})

func (AddrListTestSuite) TestAddressList(c *C) {
	al := addrList{}
	c.Assert(al.Set("/ip4/127.0.0.1/tcp/6668/p2p/16Uiu2HAm1PcCAcUZd6N4RZWnbmBHjb14Hm5iE98BY6xi7R4otHCP"), IsNil)
	c.Assert(al.String(), Equals, "/ip4/127.0.0.1/tcp/6668/p2p/16Uiu2HAm1PcCAcUZd6N4RZWnbmBHjb14Hm5iE98BY6xi7R4otHCP")
}
