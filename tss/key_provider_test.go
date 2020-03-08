package tss

import (
	"encoding/base64"
	"testing"

	"github.com/stretchr/testify/assert"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

type KeyProviderTestSuite struct{}

var _ = Suite(&KeyProviderTestSuite{})

func TestGetPubKeysFromPeerIDs(t *testing.T) {
	common.SetupBech32Prefix()
	input := []string{
		"16Uiu2HAmBdJRswX94UwYj6VLhh4GeUf9X3SjBRgTqFkeEMLmfk2M",
		"16Uiu2HAkyR9dsFqkj1BqKw8ZHAUU2yur6ZLRJxPTiiVYP5uBMeMG",
	}
	result, err := GetPubKeysFromPeerIDs(input)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	assert.Len(t, result, 2)
	assert.Equal(t, "thorpub1addwnpepqtctt9l4fddeh0krvdpxmqsxa5z9xsa0ac6frqfhm9fq6c6u5lck5s8fm4n", result[0])
	assert.Equal(t, "thorpub1addwnpepqga5cupfejfhtw507sh36fvwaekyjt5kwaw0cmgnpku0at2a87qqkp60t43", result[1])
	t.Logf("%+v", result)
}

func (*KeyProviderTestSuite) TestGetPriKey(c *C) {
	pk, err := GetPriKey("whatever")
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	input := base64.StdEncoding.EncodeToString([]byte("whatever"))
	pk, err = GetPriKey(input)
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	pk, err = GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(pk, NotNil)
	result, err := getPriKeyRawBytes(pk)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Assert(result, HasLen, 32)
}

func (KeyProviderTestSuite) TestGetPeerIDs(c *C) {
	pubKeys := []string{
		"thorpub1addwnpepqtctt9l4fddeh0krvdpxmqsxa5z9xsa0ac6frqfhm9fq6c6u5lck5s8fm4n",
		"thorpub1addwnpepqga5cupfejfhtw507sh36fvwaekyjt5kwaw0cmgnpku0at2a87qqkp60t43",
	}
	peers, err := GetPeerIDs(pubKeys)
	c.Assert(err, IsNil)
	c.Assert(peers, NotNil)
	c.Assert(peers, HasLen, 2)
	c.Assert(peers[0].String(), Equals, "16Uiu2HAmBdJRswX94UwYj6VLhh4GeUf9X3SjBRgTqFkeEMLmfk2M")
	c.Assert(peers[1].String(), Equals, "16Uiu2HAkyR9dsFqkj1BqKw8ZHAUU2yur6ZLRJxPTiiVYP5uBMeMG")
}
