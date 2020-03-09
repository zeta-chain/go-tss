package tss

import (
	"encoding/base64"
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
	"gitlab.com/thorchain/tss/go-tss/keysign"
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

func TestPackage(t *testing.T) {
	TestingT(t)
}

type FourNodeTestSuite struct {
	servers       []*TssServer
	ports         []int
	preParams     []*btsskeygen.LocalPreParams
	bootstrapPeer string
}

var _ = Suite(&FourNodeTestSuite{})

// setup four nodes for test
func (s *FourNodeTestSuite) SetUpTest(c *C) {
	common.InitLog("debug", true, "four_nodes_test")
	common.SetupBech32Prefix()
	s.ports = []int{
		16666, 16667, 16668, 16669,
	}
	s.bootstrapPeer = "/ip4/127.0.0.1/tcp/16666/p2p/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
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
func (s *FourNodeTestSuite) TestKeygenAndKeySign(c *C) {
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

	keysignReq := keysign.NewRequest(poolPubKey, base64.StdEncoding.EncodeToString([]byte("helloworld")), testPubKeys)
	keysignResult := make(map[int]keysign.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			res, err := s.servers[idx].KeySign(keysignReq)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = res
		}(i)
	}
	wg.Wait()
	var signature string
	for _, item := range keysignResult {
		if len(signature) == 0 {
			signature = item.S + item.R
			continue
		}
		c.Assert(signature, Equals, item.S+item.R)
	}
	// make sure we sign
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
