package common

import (
	. "gopkg.in/check.v1"
)

type BlameSuite struct{}

var _ = Suite(&BlameSuite{})

func (t *BlameSuite) TestBlame(c *C) {
	c.Check(NoBlame.IsEmpty(), Equals, true)

	blame := Blame{FailReason: "test"}
	c.Check(len(blame.String()) > 0, Equals, true)
}
