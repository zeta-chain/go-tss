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
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	zlog "github.com/rs/zerolog/log"
	"github.com/zeta-chain/go-tss/p2p"
	. "gopkg.in/check.v1"

	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/keysign"
)

type FourNodeScaleZetaSuite struct {
	servers        []*Server
	ports          []int
	preParams      []*btsskeygen.LocalPreParams
	bootstrapPeers []maddr.Multiaddr
	tssConfig      common.TssConfig
	poolPublicKey  string
	tmpDir         string
}

// Run with go test -v -gocheck.vv -gocheck.f FourNodeScaleZetaSuite .
var _ = Suite(&FourNodeScaleZetaSuite{})

// setup four nodes for test
func (s *FourNodeScaleZetaSuite) SetUpSuite(c *C) {
	common.InitLog("info", true, "four_nodes_zeta_test")
	conversion.SetupBech32Prefix()
	s.tmpDir = path.Join(os.TempDir(), "4nodes_zeta_test")
	os.RemoveAll(s.tmpDir)

	ports, err := p2p.GetFreePorts(4)
	c.Assert(err, IsNil)
	zlog.Info().Ints("ports", ports).Msg("Allocated ports for test")
	s.ports = ports

	s.bootstrapPeers, err = conversion.TestBootstrapAddrs(s.ports, testPubKeys)
	c.Assert(err, IsNil)
	s.preParams = getPreParams(c)
	s.servers = make([]*Server, partyNum)
	s.tssConfig = common.TssConfig{
		KeyGenTimeout:   90 * time.Second,
		KeySignTimeout:  20 * time.Second,
		PreParamTimeout: 5 * time.Second,
		EnableMonitor:   false,
	}

	var wg sync.WaitGroup
	for i := 0; i < partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if idx == 0 {
				s.servers[idx] = s.getTssServer(c, idx, s.tssConfig)
			} else {
				s.servers[idx] = s.getTssServer(c, idx, s.tssConfig)
			}
		}(i)

		time.Sleep(time.Second)
	}
	wg.Wait()

	for i := 0; i < partyNum; i++ {
		c.Assert(s.servers[i].Start(), IsNil)
	}

	s.doTestKeygen(c, partyVersion)
}

func (s *FourNodeScaleZetaSuite) TestManyKeySigns(c *C) {
	for i := 0; i < 50; i++ {
		c.Logf("KeySigning round %d started", i)
		startTime := time.Now()
		s.doTestKeySign(c, partyVersion)
		c.Logf("KeySigning round %d complete (took %s)", i, time.Since(startTime))
	}
}

// TestConcurrentKeySigns ensures that keysigns can be done concurrently
//
// keysigns do not wait for the prior keysign to finish unlike TestManyKeySigns
// keysigns are also submitted in reverse order to slow down keysigning
func (s *FourNodeScaleZetaSuite) TestConcurrentKeySigns(c *C) {
	for i := 0; i < 10; i++ {
		c.Logf("Concurrent keysign round %d started", i)
		s.doTestConcurrentKeySign(c, partyVersion)
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

func (s *FourNodeScaleZetaSuite) getTssServer(c *C, index int, conf common.TssConfig) *Server {
	priKey, err := conversion.GetPriKey(testPriKeyArr[index])
	c.Assert(err, IsNil)
	baseHome := path.Join(s.tmpDir, strconv.Itoa(index))
	if _, err := os.Stat(baseHome); os.IsNotExist(err) {
		err := os.MkdirAll(baseHome, os.ModePerm)
		c.Assert(err, IsNil)
	}
	whitelistedPeers := []peer.ID{}
	for _, pk := range testPubKeys {
		peer, err := conversion.Bech32PubkeyToPeerID(pk)
		c.Assert(err, IsNil)
		whitelistedPeers = append(whitelistedPeers, peer)
	}

	logger := zlog.With().
		Int("test.node_index", index).
		Int("test.node_port", s.ports[index]).
		Logger()

	networkConfig := NetworkConfig{
		TssConfig:        conf,
		ExternalIP:       "",
		Port:             s.ports[index],
		BootstrapPeers:   s.bootstrapPeers,
		WhitelistedPeers: whitelistedPeers,
	}

	instance, err := New(
		networkConfig,
		baseHome,
		priKey,
		"password",
		s.preParams[index],
		logger,
	)

	c.Assert(err, IsNil)

	return instance
}
