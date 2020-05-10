package blame

import (
	. "gopkg.in/check.v1"
)

type BlameTestSuite struct{}

var _ = Suite(&BlameTestSuite{})

func createNewNode(key string) Node {
	return Node{
		Pubkey:         key,
		BlameData:      nil,
		BlameSignature: nil,
	}
}

func (BlameTestSuite) TestBlame(c *C) {
	b := NewBlame("whatever", []Node{createNewNode("1"), createNewNode("2")})
	c.Assert(b.IsEmpty(), Equals, false)
	c.Logf("%s", b)
	b.AddBlameNodes(createNewNode("3"), createNewNode("4"))
	c.Assert(b.BlameNodes, HasLen, 4)
	b.AddBlameNodes(createNewNode("3"))
	c.Assert(b.BlameNodes, HasLen, 4)
	b.SetBlame("helloworld", nil)
	c.Assert(b.FailReason, Equals, "helloworld")
}
