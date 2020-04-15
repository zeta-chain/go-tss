package common

import (
	bkg "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/libp2p/go-libp2p-core/peer"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

type tssHelpSuite struct {
	tssCommon *TssCommon
}

var testPeers = []string{
	"16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh",
	"16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa",
	"16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp",
	"16Uiu2HAmAWKWf5vnpiAhfdSQebTbbB3Bg35qtyG7Hr4ce23VFA8V",
}

var _ = Suite(&tssHelpSuite{})

func (t *tssHelpSuite) SetUpTest(c *C) {
	broadcast := make(chan *messages.BroadcastMsgChan)
	conf := TssConfig{}
	tssCommon := NewTssCommon("123", broadcast, conf, "testID")
	p1, err := peer.Decode(testPeers[0])
	c.Assert(err, IsNil)
	p2, err := peer.Decode(testPeers[1])
	c.Assert(err, IsNil)
	p3, err := peer.Decode(testPeers[2])
	c.Assert(err, IsNil)
	tssCommon.lastUnicastPeer["testType"] = []peer.ID{p1, p2, p3}

	partiesID, localPartyID, err := GetParties(testPubKeys[:], testPubKeys[0])
	partyIDMap := SetupPartyIDMap(partiesID)
	err = SetupIDMaps(partyIDMap, tssCommon.PartyIDtoP2PID)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan bkg.LocalPartySaveData, len(partiesID))
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 3)
	keyGenParty := bkg.NewLocalParty(params, outCh, endCh)
	tssCommon.SetPartyInfo(&PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	t.tssCommon = tssCommon
}

func (t *tssHelpSuite) TestGetUnicastBlame(c *C) {
	blame, err := t.tssCommon.GetUnicastBlame("testTypeWrong")
	c.Assert(err, NotNil)
	blame, err = t.tssCommon.GetUnicastBlame("testType")
	c.Assert(err, IsNil)
	c.Assert(blame[0], Equals, testPubKeys[3])
}

func (t *tssHelpSuite) TestBroadcastBlame(c *C) {
	c1 := NewLocalCacheItem(nil, "hash123")
	t.tssCommon.unConfirmedMessages["round1"] = c1
	blames, err := t.tssCommon.GetBroadcastBlame("round1")
	c.Assert(err, IsNil)
	c.Assert(blames, HasLen, 3)

	msg := messages.WireMessage{
		Routing:   nil,
		RoundInfo: "round2",
		Message:   nil,
	}
	c2 := NewLocalCacheItem(&msg, "hash123")
	c2.ConfirmedList[testPeers[0]] = "123"
	c2.ConfirmedList[testPeers[1]] = "123"
	t.tssCommon.unConfirmedMessages["round2fromnode1"] = c2
	blames, err = t.tssCommon.GetBroadcastBlame("round2")
	c.Assert(err, IsNil)
	c.Assert(blames, HasLen, 1)
	c.Assert(blames[0], Equals, testPubKeys[3])
}
