package keysign

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"sort"
	"sync"
	"testing"
	"time"

	bc "github.com/binance-chain/tss-lib/common"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/p2p"
	"gitlab.com/thorchain/tss/go-tss/storage"
)

var (
	testPubKeys = []string{
		"thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69",
		"thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j",
		"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3",
		"thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09",
	}
	testPriKeyArr = []string{
		"6LABmWB4iXqkqOJ9H0YFEA2CSSx6bA7XAKGyI/TDtas=",
		"528pkgjuCWfHx1JihEjiIXS7jfTS/viEdAbjqVvSifQ=",
		"JFB2LIJZtK+KasK00NcNil4PRJS4c4liOnK0nDalhqc=",
		"vLMGhVXMOXQVnAE3BUU8fwNj/q0ZbndKkwmxfS5EN9Y=",
	}
)

func TestPackage(t *testing.T) {
	TestingT(t)
}

type MockLocalStateManager struct {
	file string
}

func (m *MockLocalStateManager) SaveLocalState(state storage.KeygenLocalState) error {
	return nil
}

func (m *MockLocalStateManager) GetLocalState(pubKey string) (storage.KeygenLocalState, error) {
	buf, err := ioutil.ReadFile(m.file)
	if err != nil {
		return storage.KeygenLocalState{}, err
	}
	var state storage.KeygenLocalState
	if err := json.Unmarshal(buf, &state); err != nil {
		return storage.KeygenLocalState{}, err
	}
	return state, nil
}

type TssKeysisgnTestSuite struct {
	comms     []*p2p.Communication
	partyNum  int
	stateMgrs []storage.LocalStateManager
}

var _ = Suite(&TssKeysisgnTestSuite{})

func (s *TssKeysisgnTestSuite) SetUpSuite(c *C) {
	common.SetupBech32Prefix()
	common.InitLog("info", true, "keysign_test")
}

func (s *TssKeysisgnTestSuite) SetUpTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	ports := []int{
		17666, 17667, 17668, 17669,
	}
	s.partyNum = 4
	s.comms = make([]*p2p.Communication, s.partyNum)
	s.stateMgrs = make([]storage.LocalStateManager, s.partyNum)
	bootstrapPeer := "/ip4/127.0.0.1/tcp/17666/p2p/16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh"
	multiAddr, err := maddr.NewMultiaddr(bootstrapPeer)
	c.Assert(err, IsNil)
	for i := 0; i < s.partyNum; i++ {
		buf, err := base64.StdEncoding.DecodeString(testPriKeyArr[i])
		c.Assert(err, IsNil)
		if i == 0 {
			comm, err := p2p.NewCommunication("asgard", nil, ports[i])
			c.Assert(err, IsNil)
			c.Assert(comm.Start(buf), IsNil)
			go comm.ProcessBroadcast()
			s.comms[i] = comm
			continue
		}
		comm, err := p2p.NewCommunication("asgard", []maddr.Multiaddr{multiAddr}, ports[i])
		c.Assert(err, IsNil)
		c.Assert(comm.Start(buf), IsNil)
		go comm.ProcessBroadcast()
		s.comms[i] = comm
	}

	for i := 0; i < s.partyNum; i++ {
		f := &MockLocalStateManager{
			file: fmt.Sprintf("../test_data/keysign_data/%d.json", i),
		}
		s.stateMgrs[i] = f
	}
}

func (s *TssKeysisgnTestSuite) TestSignMessage(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	sort.Strings(testPubKeys)
	req := NewRequest("thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33", "helloworld-test111", testPubKeys)
	messageID, err := common.MsgToHashString([]byte(req.Message))
	c.Assert(err, IsNil)
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keysignResult := make(map[int]*bc.SignatureData)
	conf := common.TssConfig{
		KeyGenTimeout:   120 * time.Second,
		KeySignTimeout:  120 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}

	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			var currentKeySign string
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, &currentKeySign, messageID)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()
			comm.SetSubscribe(p2p.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(p2p.TSSKeySignVerMsg, messageID, keysignMsgChannel)

			defer comm.CancelSubscribe(p2p.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(p2p.TSSKeySignVerMsg, messageID)
			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			sig, err := keysignIns.SignMessage([]byte(req.Message), localState, req.SignerPubKeys)
			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = sig
		}(i)
	}
	wg.Wait()
	var signature string
	for _, item := range keysignResult {
		if len(signature) == 0 {
			signature = string(item.S) + string(item.R)
			continue
		}
		c.Assert(signature, Equals, string(item.S)+string(item.R))
	}
}

func (s *TssKeysisgnTestSuite) TearDownTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	time.Sleep(time.Second)
	for _, item := range s.comms {
		c.Assert(item.Stop(), IsNil)
	}
}
