package tss

import (
	"encoding/hex"
	"encoding/json"
	"io/ioutil"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
)

const (
	partyNum         = 4
	testFileLocation = "../test_data"
	preParamTestFile = "preParam_test.data"
)

var (
	testPubKeys = []string{
		"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
		"thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09",
		"thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69",
		"thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j",
	}
	testPriKeyArr = []string{
		"MjQ1MDc2MmM4MjU5YjRhZjhhNmFjMmI0ZDBkNzBkOGE1ZTBmNDQ5NGI4NzM4OTYyM2E3MmI0OWMzNmE1ODZhNw==",
		"YmNiMzA2ODU1NWNjMzk3NDE1OWMwMTM3MDU0NTNjN2YwMzYzZmVhZDE5NmU3NzRhOTMwOWIxN2QyZTQ0MzdkNg==",
		"ZThiMDAxOTk2MDc4ODk3YWE0YThlMjdkMWY0NjA1MTAwZDgyNDkyYzdhNmMwZWQ3MDBhMWIyMjNmNGMzYjVhYg==",
		"ZTc2ZjI5OTIwOGVlMDk2N2M3Yzc1MjYyODQ0OGUyMjE3NGJiOGRmNGQyZmVmODg0NzQwNmUzYTk1YmQyODlmNA==",
	}
)

func TestPackage(t *testing.T) { TestingT(t) }

type FourNodeTestSuite struct {
	servers       []*TssServer
	ports         []int
	preParams     []*btsskeygen.LocalPreParams
	bootstrapPeer string
}

var _ = Suite(&FourNodeTestSuite{})

// setup four nodes for test
func (s *FourNodeTestSuite) SetUpTest(c *C) {
	common.SetupBech32Prefix()
	s.ports = []int{
		6666, 6667, 6668, 6669,
	}
	s.bootstrapPeer = "/ip4/127.0.0.1/tcp/6666/ipfs/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
	s.preParams = getPreparams(c)
	s.servers = make([]*TssServer, partyNum)
	conf := common.TssConfig{
		KeyGenTimeout:   30 * time.Second,
		KeySignTimeout:  30 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}

	for i := 0; i < partyNum; i++ {
		if i == 0 {
			s.servers[i] = s.getTssServer(c, i, conf, "")
		} else {
			s.servers[i] = s.getTssServer(c, i, conf, s.bootstrapPeer)
		}
		time.Sleep(time.Second)
	}

	for i := 0; i < partyNum; i++ {
		c.Assert(s.servers[i].Start(), IsNil)
	}
}

// generate a new key
func (s *FourNodeTestSuite) TestKeygen(c *C) {
	req := keygen.NewRequest(testPubKeys)
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keygenResult := make(map[int]keygen.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := s.servers[idx].Keygen(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keygenResult[idx] = res
		}(i)
	}
	wg.Wait()
	var poolPubKey string
	for _, item := range keygenResult {
		if len(poolPubKey) == 0 {
			poolPubKey = item.PubKey
		} else {
			c.Assert(poolPubKey, Equals, item.PubKey)
		}
	}
}

func (s *FourNodeTestSuite) TearDownTest(c *C) {
	for i := 0; i < partyNum; i++ {
		s.servers[i].Stop()
	}
}

func (s *FourNodeTestSuite) getTssServer(c *C, index int, conf common.TssConfig, bootstrap string) *TssServer {
	priKey, err := GetPriKey(testPriKeyArr[index])
	c.Assert(err, IsNil)
	baseHome := path.Join(os.TempDir(), strconv.Itoa(index))
	if _, err := os.Stat(baseHome); os.IsNotExist(err) {
		err := os.Mkdir(baseHome, os.ModePerm)
		c.Assert(err, IsNil)
	}
	var peerIDs []maddr.Multiaddr
	if len(bootstrap) > 0 {
		multiAddr, err := maddr.NewMultiaddr(bootstrap)
		c.Assert(err, IsNil)
		peerIDs = []maddr.Multiaddr{multiAddr}
	} else {
		peerIDs = nil
	}
	instance, err := NewTss(peerIDs, s.ports[index], priKey, "Asgard", baseHome, conf, s.preParams[index])
	c.Assert(err, IsNil)
	return instance
}

func getPreparams(c *C) []*btsskeygen.LocalPreParams {
	var preParamArray []*btsskeygen.LocalPreParams
	buf, err := ioutil.ReadFile(path.Join(testFileLocation, preParamTestFile))
	c.Assert(err, IsNil)
	preParamsStr := strings.Split(string(buf), "\n")
	for _, item := range preParamsStr {
		var preParam btsskeygen.LocalPreParams
		val, err := hex.DecodeString(item)
		c.Assert(err, IsNil)
		c.Assert(json.Unmarshal(val, &preParam), IsNil)
		preParamArray = append(preParamArray, &preParam)
	}
	return preParamArray
}

//
// func sendTestRequest(c *C, url string, request []byte) []byte {
// 	var resp *http.Response
// 	var err error
// 	if len(request) == 0 {
// 		resp, err = http.Get(url)
// 	} else {
// 		resp, err = http.Post(url, "application/json", bytes.NewBuffer(request))
// 		if resp.StatusCode != http.StatusOK {
// 			return nil
// 		}
// 	}
//
// 	c.Assert(err, IsNil)
// 	body, err := ioutil.ReadAll(resp.Body)
// 	c.Assert(err, IsNil)
// 	return body
// }
//
// func testKeySign(c *C, poolPubKey string, partyNum int) {
// 	var keySignRespArr []*keysign.Response
// 	var locker sync.Mutex
// 	msg := base64.StdEncoding.EncodeToString([]byte("hello"))
// 	keySignReq := keysign.Request{
// 		PoolPubKey:    poolPubKey,
// 		Message:       msg,
// 		SignerPubKeys: testPubKeys[:],
// 	}
// 	request, err := json.Marshal(keySignReq)
// 	c.Assert(err, IsNil)
// 	requestGroup := sync.WaitGroup{}
// 	for i := 0; i < partyNum; i++ {
// 		requestGroup.Add(1)
// 		go func(i int, request []byte) {
// 			defer requestGroup.Done()
// 			url := fmt.Sprintf("http://127.0.0.1:%d/keysign", baseTssPort+i)
// 			respByte := sendTestRequest(c, url, request)
// 			if nil != respByte {
// 				var tempResp keysign.Response
// 				err = json.Unmarshal(respByte, &tempResp)
// 				c.Assert(err, IsNil)
// 				locker.Lock()
// 				if len(tempResp.S) > 0 {
// 					keySignRespArr = append(keySignRespArr, &tempResp)
// 				}
// 				locker.Unlock()
// 			}
// 		}(i, request)
// 	}
// 	requestGroup.Wait()
// 	// this first node should get the empty result
//
// 	for i := 0; i < len(keySignRespArr)-1; i++ {
// 		// size of the signature should be 44
// 		c.Assert(keySignRespArr[i].S, HasLen, 44)
// 		c.Assert(keySignRespArr[i].S, Equals, keySignRespArr[i+1].S)
// 		c.Assert(keySignRespArr[i].R, Equals, keySignRespArr[i+1].R)
// 	}
// }
//
// func testKeyGen(c *C, partyNum int) string {
// 	var keyGenRespArr []*keygen.Response
// 	var locker sync.Mutex
// 	keyGenReq := keygen.Request{
// 		Keys: testPubKeys[:],
// 	}
// 	request, err := json.Marshal(keyGenReq)
// 	c.Assert(err, IsNil)
// 	requestGroup := sync.WaitGroup{}
// 	for i := 0; i < partyNum; i++ {
// 		requestGroup.Add(1)
// 		go func(i int, request []byte) {
// 			defer requestGroup.Done()
// 			url := fmt.Sprintf("http://127.0.0.1:%d/keygen", baseTssPort+i)
// 			respByte := sendTestRequest(c, url, request)
// 			var tempResp keygen.Response
// 			err = json.Unmarshal(respByte, &tempResp)
// 			c.Assert(err, IsNil)
// 			locker.Lock()
// 			keyGenRespArr = append(keyGenRespArr, &tempResp)
// 			locker.Unlock()
// 		}(i, request)
// 	}
// 	requestGroup.Wait()
// 	for i := 0; i < partyNum-1; i++ {
// 		c.Assert(keyGenRespArr[i].PubKey, Equals, keyGenRespArr[i+1].PubKey)
// 	}
// 	return keyGenRespArr[0].PubKey
// }
//
// func cleanUp(c *C, cancels []context.CancelFunc, wg *sync.WaitGroup, partyNum int) {
// 	for i := 0; i < partyNum; i++ {
// 		cancels[i]()
// 		directoryPath := path.Join(testFileLocation, strconv.Itoa(i))
// 		err := os.RemoveAll(directoryPath)
// 		c.Assert(err, IsNil)
// 	}
// 	wg.Wait()
// }
//
// // This test is to test whether p2p has unregister all the resources when Tss instance is terminated.
// // We need to close the p2p host and unregister the handler before we terminate the Tss
// // otherwise, we you start the Tss instance again, the new Tss will not receive all the p2p messages.
// // Following the previous test, we run 4 nodes keygen to check whether the previous tss instance polluted
// // the environment for running the new Tss instances.
// func (t *main.TssTestSuite) TestHttpRedoKeyGen(c *C) {
// 	_, _, cancels, wg := setupNodeForTest(c, partyNum)
// 	defer cleanUp(c, cancels, wg, partyNum)
//
// 	// test key gen.
// 	testKeyGen(c, partyNum)
// }
