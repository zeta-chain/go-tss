package go_tss

import (
	"bytes"
	"context"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/hashicorp/go-retryablehttp"
	. "gopkg.in/check.v1"
)

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func setupTssForTest(c *C) *Tss {
	tss, err := NewTss(nil, 6668, 8080)
	c.Assert(err, IsNil)
	c.Assert(tss, NotNil)
	return tss
}

func (t *TssTestSuite) TestNewTss(c *C) {

	tss, err := NewTss(nil, 6668, 12345)
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
	tss1, err := NewTss(nil, 8080, 8080)
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
	tss.localState = append(tss.localState, KeygenLocalStateItem{
		PubKey:    "helloworld",
		LocalData: keygen.LocalPartySaveData{},
		ParticipantKeys: []string{
			"key1", "key2", "key3",
		},
		LocalPartyKey: "key1",
	})

	signatureData, err = tss.signMessage(req)
	c.Assert(err, NotNil)
	c.Assert(signatureData, IsNil)
}
