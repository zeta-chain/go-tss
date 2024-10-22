package tss

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"io"
	"os"
	"path"
	"runtime"
	"sort"
	"strconv"
	"sync"
	"time"

	btsskeygen "github.com/bnb-chain/tss-lib/ecdsa/keygen"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/conversion"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

type FourNodeScaleZetaSuite struct {
	servers       []*TssServer
	ports         []int
	preParams     []*btsskeygen.LocalPreParams
	bootstrapPeer string
	tssConfig     common.TssConfig
	poolPublicKey string
	tmpDir        string
}

// Run with go test -v -gocheck.vv -gocheck.f FourNodeScaleZetaSuite .
var _ = Suite(&FourNodeScaleZetaSuite{})

// setup four nodes for test
func (s *FourNodeScaleZetaSuite) SetUpSuite(c *C) {
	common.InitLog("info", true, "four_nodes_zeta_test")
	conversion.SetupBech32Prefix()
	s.tmpDir = path.Join(os.TempDir(), "4nodes_zeta_test")
	os.RemoveAll(s.tmpDir)
	s.ports = []int{
		17666, 17667, 17668, 17669,
	}
	s.bootstrapPeer = "/ip4/127.0.0.1/tcp/17666/p2p/16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp"
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

	s.doTestKeygen(c, newJoinPartyVersion)
}

func (s *FourNodeScaleZetaSuite) TestManyKeysigns(c *C) {
	for i := 0; i < 50; i++ {
		c.Logf("Keysigning round %d started", i)
		startTime := time.Now()
		s.doTestKeySign(c, newJoinPartyVersion)
		c.Logf("Keysigning round %d complete (took %s)", i, time.Since(startTime))
	}
}

// TestConcurrentKeysigns ensures that keysigns can be done concurrently
//
// keysigns do not wait for the prior keysign to finish unlike TestManyKeysigns
// keysigns are also submitted in reverse order to slow down keysigning
func (s *FourNodeScaleZetaSuite) TestConcurrentKeysigns(c *C) {
	for i := 0; i < 10; i++ {
		c.Logf("Concurrent keysign round %d started", i)
		s.doTestConcurrentKeySign(c, newJoinPartyVersion)
		c.Logf("Concurrent keysign round %d complete", i)
	}
}

// generate a new key
func (s *FourNodeScaleZetaSuite) doTestKeygen(c *C, version string) {
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keygenResult := make(map[int]keygen.Response)
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keygen.NewRequest(copyTestPubKeys(), 10, version, common.ECDSA)
			res, err := s.servers[idx].Keygen(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keygenResult[idx] = res
		}(i)
	}
	wg.Wait()
	for _, item := range keygenResult {
		if len(s.poolPublicKey) == 0 {
			s.poolPublicKey = item.PubKey
		} else {
			c.Assert(s.poolPublicKey, Equals, item.PubKey)
		}
	}
}

func randomHash() []byte {
	hasher := sha256.New()
	_, err := io.CopyN(hasher, rand.Reader, 32)
	if err != nil {
		panic(err)
	}
	return hasher.Sum(nil)
}

func genMessages() []string {
	msgs := []string{
		base64.StdEncoding.EncodeToString(randomHash()),
		base64.StdEncoding.EncodeToString(randomHash()),
	}
	// input needs to be sorted otherwise you hit the race detector
	// since the input slice is sorted in place
	sort.Strings(msgs)
	return msgs
}

// test key signing
func (s *FourNodeScaleZetaSuite) doTestKeySign(c *C, version string) {
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}

	keysignResult := make(map[int]keysign.Response)
	messages := genMessages()
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			req := keysign.NewRequest(s.poolPublicKey, messages, 10, copyTestPubKeys(), version)
			res, err := s.servers[idx].KeySign(req)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = res
		}(i)
	}
	wg.Wait()
	checkSignResult(c, keysignResult)
}

func (s *FourNodeScaleZetaSuite) doTestConcurrentKeySign(c *C, version string) {
	// if this increases to 15, the tests will start to fail
	// it needs to be set quite low in CI since there are less CPUs
	numMessages := runtime.NumCPU()
	var allMessages [][]string
	for i := 0; i < numMessages; i++ {
		allMessages = append(allMessages, genMessages())
	}

	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keysignResult := make(map[int]map[int]keysign.Response)
	for msgIdx := 0; msgIdx < numMessages; msgIdx++ {
		msgIdx := msgIdx
		keysignResult[msgIdx] = make(map[int]keysign.Response)
		for partyIdx := 0; partyIdx < partyNum; partyIdx++ {
			wg.Add(1)
			// even nodes will sign messages in reverse order
			realMsgIdx := msgIdx
			if partyIdx%2 == 0 {
				realMsgIdx = numMessages - 1 - msgIdx
			}
			messages := allMessages[realMsgIdx]

			go func(idx int) {
				defer wg.Done()
				req := keysign.NewRequest(s.poolPublicKey, messages, 10, copyTestPubKeys(), version)
				res, err := s.servers[idx].KeySign(req)
				c.Assert(err, IsNil)
				lock.Lock()
				defer lock.Unlock()
				keysignResult[realMsgIdx][idx] = res
			}(partyIdx)
		}
	}
	wg.Wait()
	for _, result := range keysignResult {
		checkSignResult(c, result)
	}
}

func (s *FourNodeScaleZetaSuite) TearDownSuite(c *C) {
	// give a second before we shutdown the network
	time.Sleep(time.Second)
	for i := 0; i < partyNum; i++ {
		s.servers[i].Stop()
	}
	os.RemoveAll(s.tmpDir)
}

func (s *FourNodeScaleZetaSuite) getTssServer(c *C, index int, conf common.TssConfig, bootstrap string) *TssServer {
	priKey, err := conversion.GetPriKey(testPriKeyArr[index])
	c.Assert(err, IsNil)
	baseHome := path.Join(s.tmpDir, strconv.Itoa(index))
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
	instance, err := NewTss(peerIDs, s.ports[index], priKey, "Zeta", baseHome, conf, s.preParams[index], "", "password", []string{}, true)
	c.Assert(err, IsNil)
	return instance
}
