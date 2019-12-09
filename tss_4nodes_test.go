package tss

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/hashicorp/go-retryablehttp"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
)

const testPriKey0 = "MjQ1MDc2MmM4MjU5YjRhZjhhNmFjMmI0ZDBkNzBkOGE1ZTBmNDQ5NGI4NzM4OTYyM2E3MmI0OWMzNmE1ODZhNw=="
const testPriKey1 = "YmNiMzA2ODU1NWNjMzk3NDE1OWMwMTM3MDU0NTNjN2YwMzYzZmVhZDE5NmU3NzRhOTMwOWIxN2QyZTQ0MzdkNg=="
const testPriKey2 = "ZThiMDAxOTk2MDc4ODk3YWE0YThlMjdkMWY0NjA1MTAwZDgyNDkyYzdhNmMwZWQ3MDBhMWIyMjNmNGMzYjVhYg=="
const testPriKey3 = "ZTc2ZjI5OTIwOGVlMDk2N2M3Yzc1MjYyODQ0OGUyMjE3NGJiOGRmNGQyZmVmODg0NzQwNmUzYTk1YmQyODlmNA=="
const peerID = "/ip4/127.0.0.1/tcp/6666/ipfs/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
const baseP2PPort = 6666
const baseTssPort = 1200
const partyNum = 4
const testFileLocation = "./test_data"
const preParamTestFile = "preParam_test.data"

var testPubKeys = [...] string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}
var testPriKeyArr = [...]string{testPriKey0, testPriKey1, testPriKey2, testPriKey3}

func checkServeReady(c *C, port int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/ping", port)
	resp, err := retryablehttp.Get(url)
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode == http.StatusOK, Equals, true)
}

func startServerAndCheck(c *C, wg sync.WaitGroup, server *Tss, ctx context.Context, port int) {
	wg.Add(1)
	go func(server *Tss, ctx context.Context) {
		defer wg.Done()
		err := server.Start(ctx)
		c.Assert(err, IsNil)
	}(server, ctx)
	checkServeReady(c, port)
}

func spinUp4Servers(c *C, localTss []*Tss, ctxs []context.Context, wg sync.WaitGroup) {
	//we spin up the first signer as the "bootstrap node", and the rest 3 nodes connect to it
	startServerAndCheck(c, wg, localTss[0], ctxs[0], baseTssPort)

	for i := 1; i < partyNum; i++ {
		startServerAndCheck(c, wg, localTss[i], ctxs[i], baseTssPort+i)
	}
}

func setupContextAndNodes(c *C) ([]context.Context, []context.CancelFunc, []*Tss) {
	var localTss []*Tss
	var ctxs []context.Context
	var cancels []context.CancelFunc
	multiAddr, err := maddr.NewMultiaddr(peerID)
	c.Assert(err, IsNil)
	peerIDs := []maddr.Multiaddr{multiAddr}
	var preParamArray []*keygen.LocalPreParams
	buf, err := ioutil.ReadFile(path.Join(testFileLocation, preParamTestFile))
	c.Assert(err, IsNil)
	preParamsStr := strings.Split(string(buf), "\n")
	for _, item := range preParamsStr {
		var preParam keygen.LocalPreParams
		val, err := hex.DecodeString(string(item))
		c.Assert(err, IsNil)
		json.Unmarshal(val, &preParam)
		preParamArray = append(preParamArray, &preParam)
	}
	for i := 0; i < partyNum; i++ {
		ctx, cancel := context.WithCancel(context.Background())
		ctxs = append(ctxs, ctx)
		cancels = append(cancels, cancel)
		p2pPort := baseP2PPort + i
		tssPort := baseTssPort + i
		baseHome := path.Join(testFileLocation, strconv.Itoa(i))

		if _, err := os.Stat(baseHome); os.IsNotExist(err) {
			err := os.Mkdir(baseHome, os.ModePerm)
			c.Assert(err, IsNil)
		}
		if i == 0 {
			instance, err := NewTss(nil, p2pPort, tssPort, []byte(testPriKeyArr[i]), baseHome, *preParamArray[i])
			c.Assert(err, IsNil)
			localTss = append(localTss, instance)
		} else {
			instance, err := NewTss(peerIDs, p2pPort, tssPort, []byte(testPriKeyArr[i]), baseHome, *preParamArray[i])
			c.Assert(err, IsNil)
			localTss = append(localTss, instance)
		}
	}
	return ctxs, cancels, localTss
}

func setupNodeForTest(c *C) ([]context.Context, []context.CancelFunc, *sync.WaitGroup) {

	ctxs, cancels, localTss := setupContextAndNodes(c)
	wg := sync.WaitGroup{}
	spinUp4Servers(c, localTss, ctxs, wg)

	return ctxs, cancels, &wg
}

func sendTestRequest(c *C, url string, request []byte) []byte {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(request))
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	return body

}

func testKeySign(c *C, poolPubKey string) {
	var keySignRespArr []*KeySignResp
	var locker sync.Mutex
	msg := base64.StdEncoding.EncodeToString([]byte("hello"))
	keySignReq := KeySignReq{
		PoolPubKey: poolPubKey,
		Message:    msg,
	}
	request, err := json.Marshal(keySignReq)
	c.Assert(err, IsNil)
	requestGroup := sync.WaitGroup{}
	for i := 0; i < partyNum; i++ {
		requestGroup.Add(1)
		go func(i int, request []byte) {
			defer requestGroup.Done()
			url := fmt.Sprintf("http://127.0.0.1:%d/keysign", baseTssPort+i)
			respByte := sendTestRequest(c, url, request)
			var tempResp KeySignResp
			err = json.Unmarshal(respByte, &tempResp)
			c.Assert(err, IsNil)
			locker.Lock()
			keySignRespArr = append(keySignRespArr, &tempResp)
			locker.Unlock()
		}(i, request)
	}
	requestGroup.Wait()
	//this first node should get the empty result
	c.Assert(keySignRespArr[0].S, Equals, "")
	// size of the signature should be 44
	c.Assert(len(keySignRespArr[1].S), Equals, 44)
	for i := 1; i < partyNum-1; i++ {
		c.Assert(keySignRespArr[i].S, Equals, keySignRespArr[i+1].S)
		c.Assert(keySignRespArr[i].R, Equals, keySignRespArr[i+1].R)
	}
}

func testKeyGen(c *C) string {
	var keyGenRespArr []*KeyGenResp
	var locker sync.Mutex
	keyGenReq := KeyGenReq{
		Keys: testPubKeys[:],
	}
	request, err := json.Marshal(keyGenReq)
	c.Assert(err, IsNil)
	requestGroup := sync.WaitGroup{}
	for i := 0; i < partyNum; i++ {
		requestGroup.Add(1)
		go func(i int, request []byte) {
			defer requestGroup.Done()
			url := fmt.Sprintf("http://127.0.0.1:%d/keygen", baseTssPort+i)
			respByte := sendTestRequest(c, url, request)
			var tempResp KeyGenResp
			err = json.Unmarshal(respByte, &tempResp)
			c.Assert(err, IsNil)
			locker.Lock()
			keyGenRespArr = append(keyGenRespArr, &tempResp)
			locker.Unlock()
		}(i, request)
	}
	requestGroup.Wait()
	for i := 0; i < partyNum-1; i++ {
		c.Assert(keyGenRespArr[i].PubKey, Equals, keyGenRespArr[i+1].PubKey)
	}
	return keyGenRespArr[0].PubKey
}

func cleanUp(c *C, cancels []context.CancelFunc, wg *sync.WaitGroup) {
	for i := 0; i < partyNum; i++ {
		cancels[i]()
		directoryPath := path.Join(testFileLocation, strconv.Itoa(i))
		err := os.RemoveAll(directoryPath)
		c.Assert(err, IsNil)
	}
	wg.Wait()
}

func (t *TssTestSuite) Test4NodesKeyGenAndSign(c *C) {
	_, cancels, wg := setupNodeForTest(c)
	defer cleanUp(c, cancels, wg)
	//test key gen.
	poolPubKey := testKeyGen(c)
	//test key sign.
	testKeySign(c, poolPubKey)
}
