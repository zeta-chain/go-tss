package keysign

import (
	bc "github.com/binance-chain/tss-lib/common"
	"github.com/libp2p/go-libp2p-core/peer"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss"
)

type NotifierTestSuite struct{}

var _ = Suite(&NotifierTestSuite{})

func (NotifierTestSuite) TestNewNotifier(c *C) {
	peers := []peer.ID{
		go_tss.GetRandomPeerID(),
	}
	n, err := NewNotifier("", peers)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)
	n, err = NewNotifier("aasfdasdf", nil)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)

	n, err = NewNotifier("hello", peers)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	ch := n.GetResponseChannel()
	c.Assert(ch, NotNil)
}

func (NotifierTestSuite) TestNotifierHappyPath(c *C) {
	messageID := "messageID"
	p1, p2, p3 := go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID()
	peers := []peer.ID{
		p1, p2, p3,
	}
	n, err := NewNotifier(messageID, peers)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	s := &bc.SignatureData{
		Signature:         []byte(go_tss.RandStringBytesMask(32)),
		SignatureRecovery: []byte(go_tss.RandStringBytesMask(32)),
		R:                 []byte(go_tss.RandStringBytesMask(32)),
		S:                 []byte(go_tss.RandStringBytesMask(32)),
		M:                 []byte(go_tss.RandStringBytesMask(32)),
	}

	finish, err := n.UpdateSignature(p1, s)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	finish, err = n.UpdateSignature(p2, s)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	finish, err = n.UpdateSignature(p3, s)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, true)
	result := <-n.GetResponseChannel()
	c.Assert(result, NotNil)
	c.Assert(s == result, Equals, true)
}

func (NotifierTestSuite) TestNotifierInconsistentSignature(c *C) {
	messageID := "messageID"
	p1, p2, p3 := go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID(), go_tss.GetRandomPeerID()
	peers := []peer.ID{
		p1, p2, p3,
	}
	n, err := NewNotifier(messageID, peers)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	s := &bc.SignatureData{
		Signature:         []byte(go_tss.RandStringBytesMask(32)),
		SignatureRecovery: []byte(go_tss.RandStringBytesMask(32)),
		R:                 []byte(go_tss.RandStringBytesMask(32)),
		S:                 []byte(go_tss.RandStringBytesMask(32)),
		M:                 []byte(go_tss.RandStringBytesMask(32)),
	}
	s1 := &bc.SignatureData{
		Signature:         []byte(go_tss.RandStringBytesMask(32)),
		SignatureRecovery: []byte(go_tss.RandStringBytesMask(32)),
		R:                 []byte(go_tss.RandStringBytesMask(32)),
		S:                 []byte(go_tss.RandStringBytesMask(32)),
		M:                 []byte(go_tss.RandStringBytesMask(32)),
	}
	finish, err := n.UpdateSignature(p1, s)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	finish, err = n.UpdateSignature(p2, s)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	finish, err = n.UpdateSignature(p3, s1)
	c.Assert(err, NotNil)
	c.Assert(finish, Equals, false)
	result := <-n.GetResponseChannel()
	c.Assert(result, IsNil)
}
