package ecdsa

import (
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io/ioutil"
	"os"
	"path"
	"sort"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	tsslibcommon "github.com/bnb-chain/tss-lib/common"
	btss "github.com/bnb-chain/tss-lib/tss"
	"github.com/ipfs/go-log"
	zlog "github.com/rs/zerolog/log"

	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keysign"

	tcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/cometbft/cometbft/crypto/secp256k1"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	. "gopkg.in/check.v1"

	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/messages"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/go-tss/storage"
)

func TestOldPackage(t *testing.T) {
	TestingT(t)
}

type MockLocalStateOldManager struct {
	file string
}

func (m *MockLocalStateOldManager) SaveLocalState(state storage.KeygenLocalState) error {
	return nil
}

func (m *MockLocalStateOldManager) GetLocalState(pubKey string) (storage.KeygenLocalState, error) {
	buf, err := ioutil.ReadFile(m.file)
	if err != nil {
		return storage.KeygenLocalState{}, err
	}
	var state storage.KeygenLocalState
	if err := json.Unmarshal(buf, &state); nil != err {
		fmt.Printf("Unmarshal KeygenLocalState err %s\n", err.Error())

		var stateOld storage.KeygenLocalStateOld
		if err := json.Unmarshal(buf, &stateOld); nil != err {
			return storage.KeygenLocalState{}, fmt.Errorf(
				"fail to unmarshal KeygenLocalState with backwards compatibility: %w",
				err,
			)
		}

		state.PubKey = stateOld.PubKey
		state.ParticipantKeys = stateOld.ParticipantKeys
		state.LocalPartyKey = stateOld.LocalPartyKey
		state.LocalData, err = json.Marshal(stateOld.LocalData)

		if err != nil {
			return storage.KeygenLocalState{}, fmt.Errorf(
				"fail to marshal KeygenLocalState.LocalData for backwards compatibility: %w",
				err,
			)
		}
	}
	return state, nil
}

func (s *MockLocalStateOldManager) SaveAddressBook(address map[peer.ID][]maddr.Multiaddr) error {
	return nil
}

func (s *MockLocalStateOldManager) RetrieveP2PAddresses() ([]maddr.Multiaddr, error) {
	return nil, os.ErrNotExist
}

type TssECDSAKeysignOldTestSuite struct {
	comms        []*p2p.Communication
	partyNum     int
	stateMgrs    []storage.LocalStateManager
	nodePrivKeys []tcrypto.PrivKey
	targetPeers  []peer.ID
}

var _ = Suite(&TssECDSAKeysignOldTestSuite{})

func (s *TssECDSAKeysignOldTestSuite) SetUpSuite(c *C) {
	conversion.SetupBech32Prefix()
	common.InitLog("info", true, "keysign_test")

	for _, el := range testNodePrivkey {
		priHexBytes, err := base64.StdEncoding.DecodeString(el)
		c.Assert(err, IsNil)
		rawBytes, err := hex.DecodeString(string(priHexBytes))
		c.Assert(err, IsNil)
		var priKey secp256k1.PrivKey
		priKey = rawBytes[:32]
		s.nodePrivKeys = append(s.nodePrivKeys, priKey)
	}

	for _, el := range targets {
		p, err := peer.Decode(el)
		c.Assert(err, IsNil)
		s.targetPeers = append(s.targetPeers, p)
	}
}

func (s *TssECDSAKeysignOldTestSuite) SetUpTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}

	ports, err := p2p.GetFreePorts(4)
	c.Assert(err, IsNil)
	zlog.Info().Ints("ports", ports).Msg("Allocated ports for test")

	s.partyNum = 4
	s.comms = make([]*p2p.Communication, s.partyNum)
	s.stateMgrs = make([]storage.LocalStateManager, s.partyNum)
	bootstrapPeers, err := conversion.TestBootstrapAddrs(ports, testPubKeys)
	c.Assert(err, IsNil)
	whitelistedPeers := []peer.ID{}
	for _, pk := range testPubKeys {
		peer, err := conversion.Bech32PubkeyToPeerID(pk)
		c.Assert(err, IsNil)
		whitelistedPeers = append(whitelistedPeers, peer)
	}
	for i := 0; i < s.partyNum; i++ {
		buf, err := base64.StdEncoding.DecodeString(testPriKeyArr[i])
		c.Assert(err, IsNil)
		if i == 0 {
			comm, err := p2p.NewCommunication(nil, ports[i], "", whitelistedPeers, zlog.Logger)
			c.Assert(err, IsNil)
			c.Assert(comm.Start(buf), IsNil)
			s.comms[i] = comm
			continue
		}
		comm, err := p2p.NewCommunication(bootstrapPeers, ports[i], "", whitelistedPeers, zlog.Logger)
		c.Assert(err, IsNil)
		c.Assert(comm.Start(buf), IsNil)
		s.comms[i] = comm
	}

	for i := 0; i < s.partyNum; i++ {
		f := &MockLocalStateOldManager{
			file: fmt.Sprintf("../../test_data/keysign_data/ecdsa-old/%d.json", i),
		}
		s.stateMgrs[i] = f
	}
}

func (s *TssECDSAKeysignOldTestSuite) TestSignMessage(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	log.SetLogLevel("tss-lib", "info")
	sort.Strings(testPubKeys)
	req := keysign.NewRequest(
		"thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33",
		[]string{"helloworld-test", "t"},
		10,
		testPubKeys,
		"",
	)
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)
	wg := sync.WaitGroup{}
	lock := &sync.Mutex{}
	keysignResult := make(map[int][]*tsslibcommon.SignatureData)
	conf := common.TssConfig{
		KeyGenTimeout:   90 * time.Second,
		KeySignTimeout:  90 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}
	var msgForSign [][]byte
	msgForSign = append(msgForSign, []byte(req.Messages[0]))
	msgForSign = append(msgForSign, []byte(req.Messages[1]))
	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, messageID,
				s.nodePrivKeys[idx],
				s.comms[idx],
				s.stateMgrs[idx],
				2,
				zlog.Logger,
			)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			sig, err := keysignIns.SignMessage(msgForSign, localState, req.SignerPubKeys)

			c.Assert(err, IsNil)
			lock.Lock()
			defer lock.Unlock()
			keysignResult[idx] = sig
		}(i)
	}
	wg.Wait()

	var signatures []string
	for _, item := range keysignResult {
		if len(signatures) == 0 {
			for _, each := range item {
				signatures = append(signatures, string(each.GetSignature()))
			}
			continue
		}
		var targetSignatures []string
		for _, each := range item {
			targetSignatures = append(targetSignatures, string(each.GetSignature()))
		}

		c.Assert(signatures, DeepEquals, targetSignatures)

	}
}

func (s *TssECDSAKeysignOldTestSuite) TestSignMessageWithStop(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	sort.Strings(testPubKeys)
	req := keysign.NewRequest(
		"thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33",
		[]string{"helloworld-test", "t"},
		10,
		testPubKeys,
		"",
	)
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)

	wg := sync.WaitGroup{}
	conf := common.TssConfig{
		KeyGenTimeout:   10 * time.Second,
		KeySignTimeout:  20 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}

	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan, messageID,
				s.nodePrivKeys[idx],
				s.comms[idx],
				s.stateMgrs[idx],
				2,
				zlog.Logger,
			)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			if idx == 1 {
				go observeAndStop(c, keysignIns, stopChan)
			}

			var msgsToSign [][]byte
			msgsToSign = append(msgsToSign, []byte(req.Messages[0]))
			msgsToSign = append(msgsToSign, []byte(req.Messages[1]))

			_, err = keysignIns.SignMessage(msgsToSign, localState, req.SignerPubKeys)
			c.Assert(err, NotNil)
			lastMsg := keysignIns.tssCommonStruct.GetBlameMgr().GetLastMsg()
			zlog.Info().
				Msgf("%s------->last message %v, broadcast? %v", keysignIns.tssCommonStruct.GetLocalPeerID(), lastMsg.Type(), lastMsg.IsBroadcast())
			// we skip the node 1 as we force it to stop
			if idx != 1 {
				blames := keysignIns.GetTssCommonStruct().GetBlameMgr().GetBlame().BlameNodes
				c.Assert(blames, HasLen, 1)
				c.Assert(blames[0].Pubkey, Equals, testPubKeys[1])
			}
		}(i)
	}
	wg.Wait()
}

func (s *TssECDSAKeysignOldTestSuite) TestSignMessageRejectOnePeer(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	sort.Strings(testPubKeys)
	req := keysign.NewRequest(
		"thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33",
		[]string{"helloworld-test", "t"},
		10,
		testPubKeys,
		"",
	)
	sort.Strings(req.Messages)
	dat := []byte(strings.Join(req.Messages, ","))
	messageID, err := common.MsgToHashString(dat)
	c.Assert(err, IsNil)

	wg := sync.WaitGroup{}
	conf := common.TssConfig{
		KeyGenTimeout:   20 * time.Second,
		KeySignTimeout:  20 * time.Second,
		PreParamTimeout: 5 * time.Second,
	}
	for i := 0; i < s.partyNum; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			comm := s.comms[idx]
			stopChan := make(chan struct{})
			keysignIns := NewTssKeySign(comm.GetLocalPeerID(),
				conf,
				comm.BroadcastMsgChan,
				stopChan,
				messageID,
				s.nodePrivKeys[idx],
				s.comms[idx],
				s.stateMgrs[idx],
				2,
				zlog.Logger,
			)
			keysignMsgChannel := keysignIns.GetTssKeySignChannels()

			comm.SetSubscribe(messages.TSSKeySignMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSKeySignVerMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSControlMsg, messageID, keysignMsgChannel)
			comm.SetSubscribe(messages.TSSTaskDone, messageID, keysignMsgChannel)
			defer comm.CancelSubscribe(messages.TSSKeySignMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSKeySignVerMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSControlMsg, messageID)
			defer comm.CancelSubscribe(messages.TSSTaskDone, messageID)

			localState, err := s.stateMgrs[idx].GetLocalState(req.PoolPubKey)
			c.Assert(err, IsNil)
			if idx == 1 {
				go rejectSendToOnePeer(c, keysignIns, stopChan, s.targetPeers)
			}
			var msgsToSign [][]byte
			msgsToSign = append(msgsToSign, []byte(req.Messages[0]))
			msgsToSign = append(msgsToSign, []byte(req.Messages[1]))
			_, err = keysignIns.SignMessage(msgsToSign, localState, req.SignerPubKeys)
			lastMsg := keysignIns.tssCommonStruct.GetBlameMgr().GetLastMsg()
			zlog.Info().
				Msgf("%s------->last message %v, broadcast? %v", keysignIns.tssCommonStruct.GetLocalPeerID(), lastMsg.Type(), lastMsg.IsBroadcast())
			c.Assert(err, IsNil)
		}(i)
	}
	wg.Wait()
}

func (s *TssECDSAKeysignOldTestSuite) TearDownSuite(c *C) {
	for i, _ := range s.comms {
		tempFilePath := path.Join(os.TempDir(), strconv.Itoa(i))
		err := os.RemoveAll(tempFilePath)
		c.Assert(err, IsNil)
	}
}

func (s *TssECDSAKeysignOldTestSuite) TearDownTest(c *C) {
	if testing.Short() {
		c.Skip("skip the test")
		return
	}
	time.Sleep(time.Second)
	for _, item := range s.comms {
		c.Assert(item.Stop(), IsNil)
	}
}

func (s *TssECDSAKeysignOldTestSuite) TestCloseKeySignnotifyChannel(c *C) {
	conf := common.TssConfig{}
	keySignInstance := NewTssKeySign(
		"",
		conf,
		nil,
		nil,
		"test",
		s.nodePrivKeys[0],
		s.comms[0],
		s.stateMgrs[0],
		1,
		zlog.Logger,
	)

	taskDone := messages.TssTaskNotifier{TaskDone: true}
	taskDoneBytes, err := json.Marshal(taskDone)
	c.Assert(err, IsNil)

	msg := &messages.WrappedMessage{
		MessageType: messages.TSSTaskDone,
		MsgID:       "test",
		Payload:     taskDoneBytes,
	}
	partyIdMap := make(map[string]*btss.PartyID)
	partyIdMap["1"] = nil
	partyIdMap["2"] = nil
	fakePartyInfo := &common.PartyInfo{
		PartyMap:   nil,
		PartyIDMap: partyIdMap,
	}
	keySignInstance.tssCommonStruct.SetPartyInfo(fakePartyInfo)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node1")
	c.Assert(err, IsNil)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node2")
	c.Assert(err, IsNil)
	err = keySignInstance.tssCommonStruct.ProcessOneMessage(msg, "node1")
	c.Assert(err, ErrorMatches, "duplicated notification from peer node1")
}
