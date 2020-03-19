package p2p

import (
	"math/big"

	btss "github.com/binance-chain/tss-lib/tss"
	. "gopkg.in/check.v1"
)

type THORChainTSSMessageTypeSuite struct{}

var _ = Suite(&THORChainTSSMessageTypeSuite{})

func (THORChainTSSMessageTypeSuite) TestTHORChainTSSMessageType_String(c *C) {
	m := map[THORChainTSSMessageType]string{
		TSSKeyGenMsg:     "TSSKeyGenMsg",
		TSSKeySignMsg:    "TSSKeySignMsg",
		TSSKeyGenVerMsg:  "TSSKeyGenVerMsg",
		TSSKeySignVerMsg: "TSSKeySignVerMsg",
	}
	for k, v := range m {
		c.Assert(k.String(), Equals, v)
	}
}

func (THORChainTSSMessageTypeSuite) TestWireMessage(c *C) {
	bi := new(big.Int).SetBytes([]byte("whatever"))
	wm := WireMessage{
		Routing: &btss.MessageRouting{
			From:                    btss.NewPartyID("1", "", bi),
			To:                      nil,
			IsBroadcast:             true,
			IsToOldCommittee:        false,
			IsToOldAndNewCommittees: false,
		},
		RoundInfo: "hello",
		Message:   nil,
	}
	cacheKey := wm.GetCacheKey()
	c.Assert(cacheKey, Equals, "1-hello")
}
