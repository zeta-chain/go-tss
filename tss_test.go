package tss

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sort"
	"sync"
	"testing"

	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func setupTssForTest(c *C) *TssServer {
	protocolID := protocol.ConvertFromStrings([]string{"tss"})[0]
	ByPassGeneratePreParam = true
	homeBase := ""
	tss, err := NewTss(nil, 6668, 8080, protocolID, []byte(testPriKey), "Asgard", homeBase)
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	return tss
}

func (t *TssTestSuite) TestTssReusePort(c *C) {
	protocolID := protocol.ConvertFromStrings([]string{"tss"})[0]
	ByPassGeneratePreParam = true
	tss1, err := NewTss(nil, 6660, 8080, protocolID, []byte(testPriKey), "Asgard", "")
	c.Assert(err, IsNil)
	wg := sync.WaitGroup{}
	ctx, cancel := context.WithCancel(context.Background())

	wg.Add(1)
	go func() {
		defer wg.Done()
		err = tss1.Start(ctx)
		c.Assert(err, IsNil)
	}()
	_, err = retryablehttp.Get("http://127.0.0.1:8080/ping")
	c.Assert(err, IsNil)

	tss2, err := NewTss(nil, 6661, 8080, protocolID, []byte(testPriKey), "Asgard", "")
	ctx2, cancel2 := context.WithCancel(context.Background())
	err = tss2.Start(ctx2)
	c.Assert(err, ErrorMatches, "listen tcp :8080: bind: address already in use")
	cancel2()
	cancel()
	wg.Wait()
}

func (t *TssTestSuite) TestNewTss(c *C) {
	protocolID := protocol.ConvertFromStrings([]string{"tss"})[0]
	tss, err := NewTss(nil, 6668, 12345, protocolID, []byte(testPriKey), "Asgard", "")
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	ctx, cancel := context.WithCancel(context.Background())
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		err = tss.Start(ctx)
		c.Assert(err, IsNil)
	}()
	resp, err := retryablehttp.Get("http://127.0.0.1:12345/ping")
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode, Equals, http.StatusOK)
	resp, err = http.Get("http://127.0.0.1:12345/p2pid")
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode, Equals, http.StatusOK)
	result, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	c.Assert(len(result) > 0, Equals, true)
	c.Logf("p2p peer id: %s", result)
	cancel()
	wg.Wait()

	// P2p port and http port can't be the same
	tss1, err := NewTss(nil, 8080, 8080, protocolID, []byte(testPriKey), "Asgard", "")
	c.Assert(err, NotNil)
	c.Assert(tss1, IsNil)
}

func (t *TssTestSuite) TestKeySign(c *C) {
	tss := setupTssForTest(c)
	c.Assert(tss, NotNil)
	respRecorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/keysign", nil)
	tss.keySign(respRecorder, req)
	c.Assert(respRecorder.Code, Equals, http.StatusMethodNotAllowed)

	respRecorder = httptest.NewRecorder()
	reqInvalidBody := httptest.NewRequest(http.MethodPost, "/keysign", bytes.NewBuffer([]byte("whatever")))
	tss.keySign(respRecorder, reqInvalidBody)
	c.Assert(respRecorder.Code, Equals, http.StatusBadRequest)
}

func (t *TssTestSuite) TestSignMessage(c *C) {
	req := keysign.KeySignReq{
		PoolPubKey: "helloworld",
		Message:    "whatever",
	}

	sk, err := common.GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	keySignInstance := keysign.NewTssKeySign("", "", sk, nil, nil)
	signatureData, err := keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
	signatureData, err = keySignInstance.SignMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
}

func (t *TssTestSuite) TestGetPriKey(c *C) {
	pk, err := common.GetPriKey("whatever")
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	input := base64.StdEncoding.EncodeToString([]byte("whatever"))
	pk, err = common.GetPriKey(input)
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	pk, err = common.GetPriKey("MmVhNTI1ZDk3N2Y1NWU3OWM3M2JhNjZiNzM2NDU0ZGI2Mjc2NmU4ZTMzMzg2ZDlhZGM4YmI2MjE2NmRiMWFkMQ==")
	c.Assert(err, IsNil)
	c.Assert(pk, NotNil)
	result, err := common.GetPriKeyRawBytes(pk)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Assert(result, HasLen, 32)
}

func doNodeSyncTest(c *C, peers []peer.ID, targets []peer.ID) {
	tssCommonStruct := common.TssCommon{
		PartyIDtoP2PID: nil,
		TssMsg:         nil,
		P2PPeers:       peers,
	}
	receiver := make(chan *p2p.Message, 4)
	var expected []string
	for _, each := range targets {
		msg := p2p.Message{
			PeerID:  each,
			Payload: []byte("hello"),
		}
		receiver <- &msg
		expected = append(expected, each.String())
	}
	returnedPeers, err := tssCommonStruct.NodeSync(receiver, p2p.TSSKeySignVerMsg)
	sort.Strings(returnedPeers)
	sort.Strings(expected)
	c.Assert(returnedPeers, DeepEquals, expected)
	if len(peers) == len(targets) {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, NotNil)
	}
}

func (t *TssTestSuite) TestNodeSync(c *C) {
	//in test, we pretend to be node1
	node2, err := peer.IDB58Decode("16Uiu2HAmAWKWf5vnpiAhfdSQebTbbB3Bg35qtyG7Hr4ce23VFA8V")
	c.Assert(err, IsNil)
	node3, err := peer.IDB58Decode("16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh")
	c.Assert(err, IsNil)
	node4, err := peer.IDB58Decode("16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa")
	c.Assert(err, IsNil)

	peers := []peer.ID{node2, node3, node4}
	target := []peer.ID{node4}
	doNodeSyncTest(c, peers, target)
	target = []peer.ID{node2, node4}
	doNodeSyncTest(c, peers, target)
	// in the 4 nodes scenario, we need to have the response of 3 nodes(loopback msg is not broadcast in the p2p)
	target = []peer.ID{node2, node3, node4}
	doNodeSyncTest(c, peers, target)

}

func (t *TssTestSuite) TestMsgToHashInt(c *C) {
	input := []byte("whatever")
	result, err := common.MsgToHashInt(input)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
}

func (t *TssTestSuite) TestGetThreshold(c *C) {
	_, err := common.GetThreshold(-2)
	c.Assert(err, NotNil)
	output, err := common.GetThreshold(4)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 2)
	output, err = common.GetThreshold(9)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 5)
	output, err = common.GetThreshold(10)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 6)
	output, err = common.GetThreshold(99)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 65)
}

func (t *TssTestSuite) TestContains(c *C) {
	t1 := btss.PartyID{
		Index: 1,
	}
	ret := common.Contains(nil, &t1)
	c.Assert(ret, Equals, false)

	t2 := btss.PartyID{
		Index: 2,
	}
	t3 := btss.PartyID{
		Index: 3,
	}
	testParties := []*btss.PartyID{&t2, &t3}
	ret = common.Contains(testParties, &t1)
	c.Assert(ret, Equals, false)
	testParties = append(testParties, &t1)
	ret = common.Contains(testParties, &t1)
	c.Assert(ret, Equals, true)
	ret = common.Contains(testParties, nil)
	c.Assert(ret, Equals, false)
}
