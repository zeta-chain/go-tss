package common

import (
	"encoding/json"
	"sync"

	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

type TssCommonTestSuite struct{}

var _ = Suite(&TssCommonTestSuite{})

func (TssCommonTestSuite) TestTssCommon(c *C) {
	pk, err := sdk.GetAccPubKeyBech32("thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3")
	c.Assert(err, IsNil)
	peerID, err := GetPeerIDFromSecp256PubKey(pk.(secp256k1.PubKeySecp256k1))
	c.Assert(err, IsNil)
	broadcastChannel := make(chan *p2p.BroadcastMsgChan)
	tssCommon := NewTssCommon(peerID.String(), broadcastChannel, TssConfig{}, "message-id")
	c.Assert(tssCommon, NotNil)
	stopchan := make(chan struct{})
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		tssCommon.ProcessInboundMessages(stopchan)
	}()
	bi, err := MsgToHashInt([]byte("whatever"))
	c.Assert(err, IsNil)
	wrapMsg := fabricateTssMsg(c, btss.NewPartyID("1,", "test", bi), "roundInfo", "message")
	buf, err := json.Marshal(wrapMsg)
	c.Assert(err, IsNil)
	pMsg := &p2p.Message{
		PeerID:  peerID,
		Payload: buf,
	}

	tssCommon.partyInfo = &PartyInfo{
		Party:      nil,
		PartyIDMap: make(map[string]*btss.PartyID),
	}
	tssCommon.TssMsg <- pMsg
	close(stopchan)
	wg.Wait()
}
