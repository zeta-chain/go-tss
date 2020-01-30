package keysign

import (
	"testing"

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	common.InitLog("info", true, "keysign_test")
}

func (t *TssTestSuite) TestSignMessage(c *C) {
	req := KeySignReq{
		PoolPubKey: "helloworld",
		Message:    "whatever",
	}
	conf := common.TssConfig{}
	sk, err := common.GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	keySignInstance := NewTssKeySign("", "", conf, sk, nil, nil, nil)
	signatureData, err := keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
	signatureData, err = keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
}
