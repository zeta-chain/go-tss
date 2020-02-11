package keygen

import (
	"testing"

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

var testPubKeys = []string{
	"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
	"thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09",
	"thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69",
	"thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j",
}

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	common.InitLog("info", true, "keygen_test")
}

func (t *TssTestSuite) TestSignMessage(c *C) {
	req := Request{
		Keys: testPubKeys[:],
	}
	conf := common.TssConfig{}
	stateManager := &storage.MockLocalStateManager{}
	keyGenInstance := NewTssKeyGen("", conf, "", nil, nil, nil, nil, "test", stateManager)
	signatureData, err := keyGenInstance.GenerateNewKey(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
	generatedKey, err := keyGenInstance.GenerateNewKey(req)
	c.Assert(err, NotNil)
	c.Assert(generatedKey, IsNil)
}