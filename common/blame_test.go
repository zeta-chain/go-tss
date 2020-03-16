package common

import (
	. "gopkg.in/check.v1"
)

type BlameTestSuite struct{}

var _ = Suite(&BlameTestSuite{})

func (BlameTestSuite) TestBlame(c *C) {
	b := NewBlame("whatever", []string{"1", "2"})
	c.Assert(b.IsEmpty(), Equals, false)
	c.Logf("%s", b)
	b.AddBlameNodes("3", "4")
	c.Assert(b.BlameNodes, HasLen, 4)
	b.AddBlameNodes("3")
	c.Assert(b.BlameNodes, HasLen, 4)
	b.SetBlame("helloworld", nil)
	c.Assert(b.FailReason, Equals, "helloworld")
}
