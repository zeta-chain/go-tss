package keysign

import (
	"encoding/json"
	"io/ioutil"

	bc "github.com/binance-chain/tss-lib/common"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/common"
)

type NotifierTestSuite struct{}

var _ = Suite(&NotifierTestSuite{})

func (*NotifierTestSuite) SetUpSuite(c *C) {
	common.SetupBech32Prefix()
}

func (NotifierTestSuite) TestNewNotifier(c *C) {
	poolPubKey := go_tss.GetRandomPubKey()
	n, err := NewNotifier("", []byte("hello"), poolPubKey)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)
	n, err = NewNotifier("aasfdasdf", nil, poolPubKey)
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)

	n, err = NewNotifier("hello", []byte("hello"), "")
	c.Assert(err, NotNil)
	c.Assert(n, IsNil)

	n, err = NewNotifier("hello", []byte("hello"), poolPubKey)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	ch := n.GetResponseChannel()
	c.Assert(ch, NotNil)
}

func (NotifierTestSuite) TestNotifierHappyPath(c *C) {
	messageToSign := "helloworld-test"
	messageID, err := common.MsgToHashString([]byte(messageToSign))
	c.Assert(err, IsNil)
	poolPubKey := `thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33`
	n, err := NewNotifier(messageID, []byte(messageToSign), poolPubKey)
	c.Assert(err, IsNil)
	c.Assert(n, NotNil)
	sigFile := "../test_data/signature_notify/sig1.json"
	content, err := ioutil.ReadFile(sigFile)
	c.Assert(err, IsNil)
	c.Assert(content, NotNil)
	var signature bc.SignatureData
	err = json.Unmarshal(content, &signature)
	c.Assert(err, IsNil)

	sigInvalidFile := `../test_data/signature_notify/sig_invalid.json`
	contentInvalid, err := ioutil.ReadFile(sigInvalidFile)
	c.Assert(err, IsNil)
	c.Assert(contentInvalid, NotNil)
	var sigInvalid bc.SignatureData
	c.Assert(json.Unmarshal(contentInvalid, &sigInvalid), IsNil)
	// valid keysign peer , but invalid signature we should continue to listen
	finish, err := n.ProcessSignature(&sigInvalid)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, false)
	// valid signature from a keysign peer , we should accept it and bail out
	finish, err = n.ProcessSignature(&signature)
	c.Assert(err, IsNil)
	c.Assert(finish, Equals, true)

	result := <-n.GetResponseChannel()
	c.Assert(result, NotNil)
	c.Assert(&signature == result, Equals, true)
}
