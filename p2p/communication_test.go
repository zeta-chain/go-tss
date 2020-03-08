package p2p

import (
	. "gopkg.in/check.v1"
)

type CommunicationTestSuite struct{}

var _ = Suite(&CommunicationTestSuite{})

func (CommunicationTestSuite) TestCommunication(c *C) {
	comm, err := NewCommunication("rendezvous", nil, 6668)
	c.Assert(err, IsNil)
	c.Assert(comm, NotNil)
	comm.SetSubscribe(TSSKeyGenMsg, "hello", make(chan *Message))
	c.Assert(comm.getSubscriber(TSSKeySignMsg, "hello"), IsNil)
	c.Assert(comm.getSubscriber(TSSKeyGenMsg, "hello"), NotNil)
	comm.CancelSubscribe(TSSKeyGenMsg, "hello")
	comm.CancelSubscribe(TSSKeyGenMsg, "whatever")
	comm.CancelSubscribe(TSSKeySignMsg, "asdsdf")
}
