package tss

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"net/http"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/hashicorp/go-retryablehttp"
	"github.com/libp2p/go-libp2p-core/protocol"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

const testPriKey0 = "MjQ1MDc2MmM4MjU5YjRhZjhhNmFjMmI0ZDBkNzBkOGE1ZTBmNDQ5NGI4NzM4OTYyM2E3MmI0OWMzNmE1ODZhNw=="
const testPriKey1 = "YmNiMzA2ODU1NWNjMzk3NDE1OWMwMTM3MDU0NTNjN2YwMzYzZmVhZDE5NmU3NzRhOTMwOWIxN2QyZTQ0MzdkNg=="
const testPriKey2 = "ZThiMDAxOTk2MDc4ODk3YWE0YThlMjdkMWY0NjA1MTAwZDgyNDkyYzdhNmMwZWQ3MDBhMWIyMjNmNGMzYjVhYg=="
const testPriKey3 = "ZTc2ZjI5OTIwOGVlMDk2N2M3Yzc1MjYyODQ0OGUyMjE3NGJiOGRmNGQyZmVmODg0NzQwNmUzYTk1YmQyODlmNA=="
const peerID = "/ip4/127.0.0.1/tcp/6666/ipfs/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
const baseP2PPort = 6666
const baseTssPort = 1200
const baseInfoPort = 8000
const partyNum = 4
const testFileLocation = "./test_data"
const preParamTestFile = "preParam_test.data"

var testPubKeys = [...]string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}
var testPriKeyArr = [...]string{testPriKey0, testPriKey1, testPriKey2, testPriKey3}

func checkServeReady(c *C, port int) {
	url := fmt.Sprintf("http://127.0.0.1:%d/ping", port)
	resp, err := retryablehttp.Get(url)
	c.Assert(err, IsNil)
	c.Assert(resp.StatusCode == http.StatusOK, Equals, true)
}

func startServerAndCheck(c *C, wg sync.WaitGroup, server *TssServer, ctx context.Context, port int) {
	wg.Add(1)
	go func(server *TssServer, ctx context.Context) {
		defer wg.Done()
		err := server.Start(ctx)
		c.Assert(err, IsNil)
	}(server, ctx)
	checkServeReady(c, port)
}

func spinUpServers(c *C, localTss []*TssServer, ctxs []context.Context, wg sync.WaitGroup, partyNum int) {
	//we spin up the first signer as the "bootstrap node", and the rest 3 nodes connect to it
	startServerAndCheck(c, wg, localTss[0], ctxs[0], baseInfoPort)
	for i := 1; i < partyNum; i++ {
		startServerAndCheck(c, wg, localTss[i], ctxs[i], baseInfoPort+i)
	}
}

func setupContextAndNodes(c *C, partyNum int, conf common.TssConfig) ([]context.Context, []context.CancelFunc, []*TssServer) {
	var localTss []*TssServer
	var ctxs []context.Context
	var cancels []context.CancelFunc
	multiAddr, err := maddr.NewMultiaddr(peerID)
	protocolID := protocol.ConvertFromStrings([]string{"tss"})[0]
	c.Assert(err, IsNil)
	peerIDs := []maddr.Multiaddr{multiAddr}
	var preParamArray []*btsskeygen.LocalPreParams
	buf, err := ioutil.ReadFile(path.Join(testFileLocation, preParamTestFile))
	c.Assert(err, IsNil)
	preParamsStr := strings.Split(string(buf), "\n")
	for _, item := range preParamsStr {
		var preParam btsskeygen.LocalPreParams
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
		tssAddr := fmt.Sprintf(":%d", baseTssPort+i)
		infoAddr := fmt.Sprintf(":%d", baseInfoPort+i)
		baseHome := path.Join(testFileLocation, strconv.Itoa(i))
		if _, err := os.Stat(baseHome); os.IsNotExist(err) {
			err := os.Mkdir(baseHome, os.ModePerm)
			c.Assert(err, IsNil)
		}
		if i == 0 {
			instance, err := newTss(nil, p2pPort, tssAddr, infoAddr, protocolID, []byte(testPriKeyArr[i]), "Asgard", baseHome, conf, *preParamArray[i])
			c.Assert(err, IsNil)
			localTss = append(localTss, instance)
		} else {
			instance, err := newTss(peerIDs, p2pPort, tssAddr, infoAddr, protocolID, []byte(testPriKeyArr[i]), "Asgard", baseHome, conf, *preParamArray[i])
			c.Assert(err, IsNil)
			localTss = append(localTss, instance)
		}
	}
	return ctxs, cancels, localTss
}

func setupNodeForTest(c *C, partyNum int) ([]context.Context, []*TssServer, []context.CancelFunc, *sync.WaitGroup) {
	ByPassGeneratePreParam = false
	conf := common.TssConfig{
		KeyGenTimeout:   30 * time.Second,
		KeySignTimeout:  30 * time.Second,
		SyncTimeout:     5 * time.Second,
		PreParamTimeout: 5 * time.Second,
		SyncRetry:       20,
	}
	ctxs, cancels, localTss := setupContextAndNodes(c, partyNum, conf)
	wg := sync.WaitGroup{}
	spinUpServers(c, localTss, ctxs, wg, partyNum)
	return ctxs, localTss, cancels, &wg
}

func sendTestRequest(c *C, url string, request []byte) []byte {
	var resp *http.Response
	var err error
	if len(request) == 0 {
		resp, err = http.Get(url)
	} else {
		resp, err = http.Post(url, "application/json", bytes.NewBuffer(request))
	}
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	return body
}

func testKeySign(c *C, poolPubKey string, partyNum int) {
	var keySignRespArr []*keysign.KeySignResp
	var locker sync.Mutex
	msg := base64.StdEncoding.EncodeToString([]byte("hello"))
	keySignReq := keysign.KeySignReq{
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
			var tempResp keysign.KeySignResp
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
	c.Assert(keySignRespArr[1].S, HasLen, 44)
	for i := 1; i < partyNum-1; i++ {
		c.Assert(keySignRespArr[i].S, Equals, keySignRespArr[i+1].S)
		c.Assert(keySignRespArr[i].R, Equals, keySignRespArr[i+1].R)
	}
}

func testKeyGen(c *C, partyNum int) string {
	var keyGenRespArr []*keygen.KeyGenResp
	var locker sync.Mutex
	keyGenReq := keygen.KeyGenReq{
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
			var tempResp keygen.KeyGenResp
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

func cleanUp(c *C, cancels []context.CancelFunc, wg *sync.WaitGroup, partyNum int) {
	for i := 0; i < partyNum; i++ {
		cancels[i]()
		directoryPath := path.Join(testFileLocation, strconv.Itoa(i))
		err := os.RemoveAll(directoryPath)
		c.Assert(err, IsNil)
	}
	wg.Wait()
}

func (t *TssTestSuite) TestHttp4NodesTss(c *C) {
	_, _, cancels, wg := setupNodeForTest(c, partyNum)
	defer cleanUp(c, cancels, wg, partyNum)
	//test key gen.
	poolPubKey := testKeyGen(c, partyNum)
	// we test keygen and key sign running in parallel
	var wgGenSign sync.WaitGroup
	wgGenSign.Add(1)
	go func() {
		defer wgGenSign.Done()
		//test key sign.
		testKeySign(c, poolPubKey, partyNum)
	}()
	wgGenSign.Add(1)
	go func() {
		defer wgGenSign.Done()
		//test key gen.
		testKeyGen(c, partyNum)
	}()
	wgGenSign.Wait()
}

// This test is to test whether p2p has unregister all the resources when Tss instance is terminated.
// We need to close the p2p host and unregister the handler before we terminate the Tss
// otherwise, we you start the Tss instance again, the new Tss will not receive all the p2p messages.
// Following the previous test, we run 4 nodes keygen to check whether the previous tss instance polluted
// the environment for running the new Tss instances.
func (t *TssTestSuite) TestHttpRedoKeyGen(c *C) {
	_, _, cancels, wg := setupNodeForTest(c, partyNum)
	defer cleanUp(c, cancels, wg, partyNum)
	//test key gen.
	testKeyGen(c, partyNum)
}

func (t *TssTestSuite) TestTssProcessOutCh(c *C) {
	sk, err := common.GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	conf := common.TssConfig{}
	keySignInstance := keysign.NewTssKeySign("", "", conf, sk, nil, nil, nil)
	localTestPubKeys := make([]string, len(testPubKeys))
	copy(localTestPubKeys, testPubKeys[:])
	partiesID, localPartyID, err := common.GetParties(localTestPubKeys, testPubKeys[0], true)
	c.Assert(err, IsNil)
	messageRouting := btss.MessageRouting{
		From:                    localPartyID,
		To:                      partiesID[3:],
		IsBroadcast:             true,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	testFill := []byte("TEST")
	testContent := &btsskeygen.KGRound1Message{
		Commitment: testFill,
	}
	msg := btss.NewMessageWrapper(messageRouting, testContent)
	tssMsg := btss.NewMessage(messageRouting, testContent, msg)
	TssCommStruct := keySignInstance.GetTssCommonStruct()
	err = TssCommStruct.ProcessOutCh(tssMsg, p2p.TSSKeyGenMsg)
	c.Assert(err, IsNil)
}
