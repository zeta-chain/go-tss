package tss

import (
	"bytes"
	"context"
	"encoding/base64"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	btss "github.com/binance-chain/tss-lib/tss"
	"github.com/hashicorp/go-retryablehttp"

	// "bytes"
	// "context"
	// "io/ioutil"
	// "net/http"
	// "net/http/httptest"
	// "sync"
	// "testing"
	//
	// "github.com/binance-chain/tss-lib/ecdsa/keygen"
	// "github.com/hashicorp/go-retryablehttp"
	. "gopkg.in/check.v1"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func setupTssForTest(c *C) *Tss {
	byPassGeneratePreParam = true
	homeBase := ""
	tss, err := NewTss(nil, 6668, 8080, []byte(testPriKey), homeBase)
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	return tss
}

func (t *TssTestSuite) TestTssReusePort(c *C) {
	tss1, err := NewTss(nil, 6660, 8080, []byte(testPriKey), "")
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
	tss2, err := NewTss(nil, 6661, 8080, []byte(testPriKey), "")
	ctx2, cancel2 := context.WithCancel(context.Background())
	err = tss2.Start(ctx2)
	c.Assert(err.Error() == "listen tcp :8080: bind: address already in use", Equals, true)
	cancel2()
	cancel()
	wg.Wait()
}

func (t *TssTestSuite) TestNewTss(c *C) {
	tss, err := NewTss(nil, 6668, 12345, []byte(testPriKey), "")
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
	tss1, err := NewTss(nil, 8080, 8080, []byte(testPriKey), "")
	c.Assert(err, NotNil)
	c.Assert(tss1, IsNil)
}

func (t *TssTestSuite) TestKeySign(c *C) {
	tss := setupTssForTest(c)
	c.Assert(tss, NotNil)
	respRecorder := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "/keysign", nil)
	tss.keysign(respRecorder, req)
	c.Assert(respRecorder.Code, Equals, http.StatusMethodNotAllowed)

	respRecorder = httptest.NewRecorder()
	reqInvalidBody := httptest.NewRequest(http.MethodPost, "/keysign", bytes.NewBuffer([]byte("whatever")))
	tss.keysign(respRecorder, reqInvalidBody)
	c.Assert(respRecorder.Code, Equals, http.StatusBadRequest)
}

func (t *TssTestSuite) TestSignMessage(c *C) {
	tss := setupTssForTest(c)
	req := KeySignReq{
		PoolPubKey: "helloworld",
		Message:    "whatever",
	}
	signatureData, err := tss.signMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
	signatureData, err = tss.signMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
}

func (t *TssTestSuite) TestGetPriKey(c *C) {
	pk, err := getPriKey("whatever")
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	input := base64.StdEncoding.EncodeToString([]byte("whatever"))
	pk, err = getPriKey(input)
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	pk, err = getPriKey("MmVhNTI1ZDk3N2Y1NWU3OWM3M2JhNjZiNzM2NDU0ZGI2Mjc2NmU4ZTMzMzg2ZDlhZGM4YmI2MjE2NmRiMWFkMQ==")
	c.Assert(err, IsNil)
	c.Assert(pk, NotNil)
	result, err := getPriKeyRawBytes(pk)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Assert(result, HasLen, 32)
}

func (t *TssTestSuite) TestMsgToHashInt(c *C) {
	input := []byte("whatever")
	result, err := msgToHashInt(input)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
}

func (t *TssTestSuite) TestGetThreshold(c *C) {
	_, err := getThreshold(-2)
	c.Assert(err, NotNil)
	output, err := getThreshold(4)
	c.Assert(output, Equals, 2)
	output, err = getThreshold(9)
	c.Assert(output, Equals, 5)
	output, err = getThreshold(10)
	c.Assert(output, Equals, 6)
	output, err = getThreshold(99)
	c.Assert(output, Equals, 65)
}

func (t *TssTestSuite) TestContains(c *C) {
	t1 := btss.PartyID{
		Index: 1,
	}
	ret := contains(nil, &t1)
	c.Assert(ret, Equals, false)

	t2 := btss.PartyID{
		Index: 2,
	}
	t3 := btss.PartyID{
		Index: 3,
	}
	testParties := []*btss.PartyID{&t2, &t3}
	ret = contains(testParties, &t1)
	c.Assert(ret, Equals, false)
	testParties = append(testParties, &t1)
	ret = contains(testParties, &t1)
	c.Assert(ret, Equals, true)
	ret = contains(testParties, nil)
	c.Assert(ret, Equals, false)
}
