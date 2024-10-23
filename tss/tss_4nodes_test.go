package tss

import (
	"bytes"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	btsskeygen "github.com/bnb-chain/tss-lib/ecdsa/keygen"
	maddr "github.com/multiformats/go-multiaddr"

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

const (
	partyNum         = 4
	testFileLocation = "../test_data"
	preParamTestFile = "preParam_test.data"

	newJoinPartyVersion string = "0.14.0"
	oldJoinPartyVersion string = "0.13.0"
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

func copyTestPubKeys() []string {
	ret := make([]string, len(testPubKeys))
	copy(ret, testPubKeys)
	return ret
}

func TestPackage(t *testing.T) {
	TestingT(t)
}

type FourNodeTestSuite struct {
	servers       []*TssServer
	ports         []int
	preParams     []*btsskeygen.LocalPreParams
	bootstrapPeer string
	tssConfig     common.TssConfig
}

var _ = Suite(&FourNodeTestSuite{})

// setup four nodes for test
func (s *FourNodeTestSuite) SetUpTest(c *C) {
	common.InitLog("info", true, "four_nodes_test")
	conversion.SetupBech32Prefix()
	s.ports = []int{
		16666, 16667, 16668, 16669,
	}
	s.bootstrapPeer = "/ip4/127.0.0.1/tcp/16666/p2p/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
	s.preParams = getPreparams(c)
	s.servers = make([]*TssServer, partyNum)
	s.tssConfig = common.TssConfig{
		KeyGenTimeout:   90 * time.Second,
		KeySignTimeout:  90 * time.Second,
		PreParamTimeout: 5 * time.Second,
		EnableMonitor:   false,
	}

	var wg sync.WaitGroup
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx == 0 {
				s.servers[idx] = s.getTssServer(c, idx, s.tssConfig, "")
			} else {
				s.servers[idx] = s.getTssServer(c, idx, s.tssConfig, s.bootstrapPeer)
			}
		}(i)

		time.Sleep(time.Second)
	}
	wg.Wait()
	for i := 0; i < partyNum; i++ {
		c.Assert(s.servers[i].Start(), IsNil)
	}
}

func hash(payload []byte) []byte {
	h := sha256.New()
	h.Write(payload)
	return h.Sum(nil)
}

// we do for both join party schemes
func (s *FourNodeTestSuite) Test4NodesTss(c *C) {
	algos := []common.Algo{common.ECDSA, common.EdDSA}
	// 0.13.0 is oldJoinParty, 0.14.0 is the new leader-based joinParty
	versions := []string{oldJoinPartyVersion, newJoinPartyVersion}
	for _, algo := range algos {
		for _, jpv := range versions {
			c.Logf("testing with version %s for algo %s", jpv, algo)
			s.doTestKeygenAndKeySign(c, jpv, algo)
			s.doTestFailJoinParty(c, jpv, algo)
			s.doTestBlame(c, jpv, algo)
		}
	}
}

func checkSignResult(c *C, keysignResult map[int]keysign.Response) {
	for i := 0; i < len(keysignResult)-1; i++ {
		currentSignatures := keysignResult[i].Signatures
		// we test with two messsages and the size of the signature should be 44
		c.Assert(currentSignatures, HasLen, 2)
		c.Assert(currentSignatures[0].S, HasLen, 44)
		currentData, err := json.Marshal(currentSignatures)
		c.Assert(err, IsNil)
		nextSignatures := keysignResult[i+1].Signatures
		nextData, err := json.Marshal(nextSignatures)
		c.Assert(err, IsNil)
		ret := bytes.Equal(currentData, nextData)
		c.Assert(ret, Equals, true)
	}
}

// generate a new key
func (s *FourNodeTestSuite) doTestKeygenAndKeySign(c *C, version string, algo common.Algo) {
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keygenResult := make(map[int]keygen.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keygen.NewRequest(copyTestPubKeys(), 10, version, algo)
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

	keysignReqWithErr := keysign.NewRequest(poolPubKey, []string{"helloworld", "helloworld2"}, 10, copyTestPubKeys(), version)
	resp, err := s.servers[0].KeySign(keysignReqWithErr)
	c.Assert(err, NotNil)
	c.Assert(resp.Signatures, HasLen, 0)

	makeMessages := func() []string {
		return []string{
			base64.StdEncoding.EncodeToString(hash([]byte("helloworld"))),
			base64.StdEncoding.EncodeToString(hash([]byte("helloworld2"))),
		}
	}

	if version != newJoinPartyVersion {
		pubKeys1 := copyTestPubKeys()
		keysignReqWithErr1 := keysign.NewRequest(poolPubKey, makeMessages(), 10, pubKeys1[:1], oldJoinPartyVersion)
		resp, err = s.servers[0].KeySign(keysignReqWithErr1)
		c.Assert(err, NotNil)
		c.Assert(resp.Signatures, HasLen, 0)

	}
	if version != newJoinPartyVersion {
		keysignReqWithErr2 := keysign.NewRequest(poolPubKey, makeMessages(), 10, nil, oldJoinPartyVersion)
		resp, err = s.servers[0].KeySign(keysignReqWithErr2)
		c.Assert(err, NotNil)
		c.Assert(resp.Signatures, HasLen, 0)
	}

	keysignResult := make(map[int]keysign.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keysign.NewRequest(poolPubKey, makeMessages(), 10, copyTestPubKeys(), version)
			res, err := s.servers[idx].KeySign(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = res
		}(i)
	}
	wg.Wait()
	checkSignResult(c, keysignResult)

	keysignResult1 := make(map[int]keysign.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			var signers []string
			if version == oldJoinPartyVersion {
				signers = copyTestPubKeys()[:3]
			}
			req := keysign.NewRequest(poolPubKey, makeMessages(), 10, signers, version)
			res, err := s.servers[idx].KeySign(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult1[idx] = res
		}(i)
	}
	wg.Wait()
	checkSignResult(c, keysignResult1)
}

func (s *FourNodeTestSuite) doTestFailJoinParty(c *C, version string, algo common.Algo) {
	// JoinParty should fail if there is a node that suppose to be in the keygen , but we didn't send request in
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keygenResult := make(map[int]keygen.Response)
	// here we skip the first node
	for i := 1; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keygen.NewRequest(copyTestPubKeys(), 10, version, algo)
			res, err := s.servers[idx].Keygen(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keygenResult[idx] = res
		}(i)
	}

	wg.Wait()
	c.Logf("result:%+v", keygenResult)
	for _, item := range keygenResult {
		c.Assert(item.PubKey, Equals, "")
		c.Assert(item.Status, Equals, common.Fail)
		var expectedFailNode string
		if version == newJoinPartyVersion {
			c.Assert(item.Blame.BlameNodes, HasLen, 2)
			expectedFailNode := []string{
				"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
				"thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69",
			}
			c.Assert(item.Blame.BlameNodes[0].Pubkey, Equals, expectedFailNode[0])
			c.Assert(item.Blame.BlameNodes[1].Pubkey, Equals, expectedFailNode[1])
		} else {
			expectedFailNode = "thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3"
			c.Assert(item.Blame.BlameNodes[0].Pubkey, Equals, expectedFailNode)
		}
	}
}

func (s *FourNodeTestSuite) doTestBlame(c *C, version string, algo common.Algo) {
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keygenResult := make(map[int]keygen.Response)
	joinPartyChan := make(chan struct{})
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keygen.NewRequest(copyTestPubKeys(), 10, version, algo)
			s.servers[idx].setJoinPartyChan(joinPartyChan)
			res, err := s.servers[idx].Keygen(req)
			c.Assert(err, NotNil, Commentf("idx=%d", idx))
			lock.Lock()
			defer lock.Unlock()
			keygenResult[idx] = res
		}(i)
	}

	// if we shutdown one server during keygen, that server should be blamed. this channel is
	// used to ensure all Keygen calls have successfully joined a party and are ready for keygen
	// before we shut down the server.

	shutdownIdx := 0
	var numJoined int
	for range joinPartyChan {
		numJoined++
		if numJoined >= partyNum {
			break
		}
	}
	close(joinPartyChan)

	s.servers[shutdownIdx].Stop()

	defer func() {
		c.Log("restarting/resetting tss server at ", shutdownIdx)
		if shutdownIdx == 0 {
			// don't use a boostrap peer if we are shutting down the first server b/c the first
			// server is the bootstrap peer, so it doesn't work
			s.servers[shutdownIdx] = s.getTssServer(c, shutdownIdx, s.tssConfig, "")
		} else {
			s.servers[shutdownIdx] = s.getTssServer(c, shutdownIdx, s.tssConfig, s.bootstrapPeer)
		}
		c.Assert(s.servers[shutdownIdx].Start(), IsNil)

		// Unset the join channel so Keygen does not block after joinParty for other tests
		for i := 0; i < partyNum; i++ {
			s.servers[i].unsetJoinPartyChan()
		}
	}()

	wg.Wait()

	c.Logf("result:%+v", keygenResult)
	for idx, item := range keygenResult {
		if idx == shutdownIdx {
			continue
		}
		comment := Commentf("idx=%d", idx)
		c.Assert(item.PubKey, Equals, "", comment)
		c.Assert(item.Status, Equals, common.Fail, comment)
		c.Assert(item.Blame.BlameNodes, HasLen, 1, comment)
		c.Assert(item.Blame.BlameNodes[0].Pubkey, Equals, testPubKeys[shutdownIdx], comment)
	}
}

func (s *FourNodeTestSuite) TearDownTest(c *C) {
	// give a second before we shutdown the network
	time.Sleep(time.Second)
	for i := 0; i < partyNum; i++ {
		s.servers[i].Stop()
	}
	for i := 0; i < partyNum; i++ {
		tempFilePath := path.Join(os.TempDir(), "4nodes_test", strconv.Itoa(i))
		os.RemoveAll(tempFilePath)
	}
}

func (s *FourNodeTestSuite) getTssServer(c *C, index int, conf common.TssConfig, bootstrap string) *TssServer {
	priKey, err := conversion.GetPriKey(testPriKeyArr[index])
	c.Assert(err, IsNil)
	baseHome := path.Join(os.TempDir(), "4nodes_test", strconv.Itoa(index))
	if _, err := os.Stat(baseHome); os.IsNotExist(err) {
		err := os.MkdirAll(baseHome, os.ModePerm)
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
	instance, err := NewTss(peerIDs, s.ports[index], priKey, "Asgard", baseHome, conf, s.preParams[index], "", "password", []string{}, true)
	c.Assert(err, IsNil)
	return instance
}

func getPreparams(c *C) []*btsskeygen.LocalPreParams {
	var preParamArray []*btsskeygen.LocalPreParams
	buf, err := os.ReadFile(path.Join(testFileLocation, preParamTestFile))
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
