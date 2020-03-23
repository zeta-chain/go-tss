package p2p

import (
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

type CommunicationTestSuite struct{}

var _ = Suite(&CommunicationTestSuite{})

func (CommunicationTestSuite) TestCommunication(c *C) {
	comm, err := NewCommunication("rendezvous", nil, 6668)
	c.Assert(err, IsNil)
	c.Assert(comm, NotNil)
	comm.SetSubscribe(messages.TSSKeyGenMsg, "hello", make(chan *Message))
	c.Assert(comm.getSubscriber(messages.TSSKeySignMsg, "hello"), IsNil)
	c.Assert(comm.getSubscriber(messages.TSSKeyGenMsg, "hello"), NotNil)
	comm.CancelSubscribe(messages.TSSKeyGenMsg, "hello")
	comm.CancelSubscribe(messages.TSSKeyGenMsg, "whatever")
	comm.CancelSubscribe(messages.TSSKeySignMsg, "asdsdf")
}
