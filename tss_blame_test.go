// all the tests in this file are all slow tests, cause we are testing the blame scheme
// based on the feedbacks from nodes' timeout scheme

package tss

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

type BlameTestSuite struct{}

var _ = Suite(&BlameTestSuite{})

// we have these 10 fake public keys here as we simulate to test hash check happen in 10 nodes environment.
var testBlamePubKeys = []string{"thorpub1addwnpepqtr5p8tllhp4xaxmu77zhqen24pmrdlnekzevshaqkyzdqljm6rejnnt02t", "thorpub1addwnpepqwz59mn4ae82svm2pfqnycducjjeez0qlf3sum7rclrr8mn44pr5gkeey25", "thorpub1addwnpepqga4nded5hhnwsrwmrns803w7vu9mffp9r6dz4l6smaww2l5useuq6vkttg", "thorpub1addwnpepq28hfdpu3rdgvj8skzhlm8hyt5nlwwc8pjrzvn253j86e4dujj6jsmuf25q", "thorpub1addwnpepqfuq0xc67052h288r6flp67l0ny9mg6u3sxhsrlukyfg0fe9j6q36ysd33y", "thorpub1addwnpepq0jszts80udfl4pkfk6cp93647yl6fhu6pk486uwjdz2sf94qvu0kw0t6ug", "thorpub1addwnpepqw6mmffk69n5taaqhq3wsc8mvdpsrdnx960kujeh4jwm9lj8nuyux9hz5e4", "thorpub1addwnpepq0pdhm2jatzg2vy6fyw89vs6q374zayqd5498wn8ww780grq256ygq7hhjt", "thorpub1addwnpepqggwmlgd8u9t2sx4a0styqwhzrvdhpvdww7sqwnweyrh25rjwwm9q65kx9s", "thorpub1addwnpepqtssltyjvms8pa7k4yg85lnrjqtvvr2ecr36rhm7pa4ztf55tnuzzgvegpk"}

var testBlamePrivKey = "OWU2YTk1NzdlOTA5NTAxZmI4YjUyODYyMmZkYzBjNzJlMTQ5YTI2YWY5NzkzYTc0MjA3MDBkMWQzMzFiMDNhZg=="

func (t *BlameTestSuite) SetUpSuite(c *C) {
	initLog("info", true)
}

type TestParties struct {
	honest    []int
	malicious []int
}

func setupNodeBlameForTest(c *C, partyNum int) ([]context.Context, []*TssServer, []context.CancelFunc, *sync.WaitGroup) {
	conf := common.TssConfig{
		KeyGenTimeout:   5,
		KeySignTimeout:  5,
		SyncTimeout:     2,
		PreParamTimeout: 5,
		SyncRetry:       10,
	}
	ctxs, cancels, localTss := setupContextAndNodes(c, partyNum, conf)
	wg := sync.WaitGroup{}
	spinUpServers(c, localTss, ctxs, wg, partyNum)
	return ctxs, localTss, cancels, &wg
}

func blameCheck(c *C, blames []string, targets []int) {
	var targetBlamePeers []string
	for _, each := range targets {
		targetBlamePeers = append(targetBlamePeers, testPubKeys[each])
	}
	sort.Strings(targetBlamePeers)
	sort.Strings(blames)
	c.Assert(targetBlamePeers, DeepEquals, blames)
}

func doStartKeygen(c *C, i int, locker *sync.Mutex, requestGroup *sync.WaitGroup, request []byte, keyGenRespArr *[]*keygen.KeyGenResp) {

	defer requestGroup.Done()
	url := fmt.Sprintf("http://127.0.0.1:%d/keygen", baseTssPort+i)
	respByte := sendTestRequest(c, url, request)
	var tempResp keygen.KeyGenResp
	err := json.Unmarshal(respByte, &tempResp)
	c.Assert(err, IsNil)
	locker.Lock()
	*keyGenRespArr = append(*keyGenRespArr, &tempResp)
	locker.Unlock()
}

func doStartKeySign(c *C, i int, locker *sync.Mutex, requestGroup *sync.WaitGroup, request []byte, keySignRespArr *[]*keysign.KeySignResp) {

	defer requestGroup.Done()
	url := fmt.Sprintf("http://127.0.0.1:%d/keysign", baseTssPort+i)
	respByte := sendTestRequest(c, url, request)
	var tempResp keysign.KeySignResp
	err := json.Unmarshal(respByte, &tempResp)
	c.Assert(err, IsNil)
	locker.Lock()
	*keySignRespArr = append(*keySignRespArr, &tempResp)
	locker.Unlock()
}

func testBlameNodeSync(c *C, testParties TestParties, reason string) {
	var keyGenRespArr []*keygen.KeyGenResp
	var locker sync.Mutex
	keyGenReq := keygen.KeyGenReq{
		Keys: testPubKeys[:],
	}
	request, err := json.Marshal(keyGenReq)
	c.Assert(err, IsNil)
	requestGroup := sync.WaitGroup{}
	for _, partyIndex := range testParties.honest {
		requestGroup.Add(1)
		go doStartKeygen(c, partyIndex, &locker, &requestGroup, request, &keyGenRespArr)
	}
	requestGroup.Wait()

	for i := 0; i < len(testParties.honest); i++ {
		blameCheck(c, keyGenRespArr[i].Blame.BlameNodes, testParties.malicious)
		c.Assert(keyGenRespArr[i].Blame.FailReason, Equals, reason)
	}
}

func checkNodeStatus(c *C, testParties TestParties, expected uint64) {

	requestGroup := sync.WaitGroup{}
	for _, partyIndex := range testParties.honest {
		requestGroup.Add(1)
		go func(i int) {
			defer requestGroup.Done()
			url := fmt.Sprintf("http://127.0.0.1:%d/nodestatus", baseTssPort+i)
			respByte := sendTestRequest(c, url, nil)
			var tempResp common.TssStatus
			err := json.Unmarshal(respByte, &tempResp)
			c.Assert(err, IsNil)
			c.Assert(tempResp.FailedKeyGen, Equals, expected)
		}(partyIndex)
	}
	requestGroup.Wait()
}

func testNodeSyncBlame(c *C) {
	testParties := TestParties{
		honest:    []int{0, 1, 2},
		malicious: []int{3},
	}
	testBlameNodeSync(c, testParties, common.BlameNodeSyncCheck)

	testParties = TestParties{
		honest:    []int{1, 2},
		malicious: []int{0, 3},
	}
	testBlameNodeSync(c, testParties, common.BlameNodeSyncCheck)

	testParties = TestParties{
		honest:    []int{2},
		malicious: []int{0, 1, 3},
	}
	testBlameNodeSync(c, testParties, common.BlameNodeSyncCheck)
	// we run node sync test 3 times, we expect to have 3 failure logged
	checkNodeStatus(c, testParties, 3)
}

func doObserveAndStop(c *C, testParties TestParties, expected int, cancel context.CancelFunc) {

	requestGroup := sync.WaitGroup{}
	for _, partyIndex := range testParties.malicious {
		requestGroup.Add(1)
		go func(i int) {
			defer requestGroup.Done()
			for {
				url := fmt.Sprintf("http://127.0.0.1:%d/nodestatus", baseTssPort+i)
				respByte := sendTestRequest(c, url, nil)
				var tempResp common.TssStatus
				err := json.Unmarshal(respByte, &tempResp)
				c.Assert(err, IsNil)
				if len(tempResp.CurrKeySign) == 0 {
					continue
				}
				out := strings.SplitAfter(tempResp.CurrKeySign, "SignRound")
				v := out[1][:1]
				i, err := strconv.ParseInt(v, 10, 32)
				c.Assert(err, IsNil)
				if i > int64(expected) {
					cancel()
					break
				}
				time.Sleep(time.Millisecond * 5)
			}
		}(partyIndex)
	}
	requestGroup.Wait()
}

func doBlameTimeoutTest(c *C, poolPubKey string, testParties TestParties, cancel context.CancelFunc) {
	var keySignRespArr []*keysign.KeySignResp
	var locker sync.Mutex
	requestGroup := sync.WaitGroup{}
	msg := base64.StdEncoding.EncodeToString([]byte("hello"))
	keySignReq := keysign.KeySignReq{
		PoolPubKey: poolPubKey,
		Message:    msg,
	}
	request, err := json.Marshal(keySignReq)
	c.Assert(err, IsNil)

	// now we observe the malicious nodes current keygen
	// if it hits what expected, we just halt the malicious nodes
	go doObserveAndStop(c, testParties, 1, cancel)

	allNodes := append(testParties.honest, testParties.malicious[:]...)
	for _, partyIndex := range allNodes {
		requestGroup.Add(1)
		go doStartKeySign(c, partyIndex, &locker, &requestGroup, request, &keySignRespArr)
	}
	requestGroup.Wait()
	for i := 0; i < len(testParties.honest)+len(testParties.malicious); i++ {
		// honest nodes should have the blame nodes
		if len(keySignRespArr[i].Blame.BlameNodes) > 0 {
			blameCheck(c, keySignRespArr[i].Blame.BlameNodes, testParties.malicious)
			c.Assert(keySignRespArr[i].Blame.FailReason, Equals, common.BlameTssTimeout)
		}
	}
}

func testKeySignBlameTimeout(c *C, poolPubKey string, cancels []context.CancelFunc) {
	testParties := TestParties{
		honest:    []int{0, 3},
		malicious: []int{2},
	}
	//in our test, we only have 4 nodes, so we can only have one malicious node
	nodeIndex := testParties.malicious[0]
	doBlameTimeoutTest(c, poolPubKey, testParties, cancels[nodeIndex])
}

//func (t *BlameTestSuite) TestNodeSyncAndTimeoutBlame(c *C) {
//	ByPassGeneratePreParam = false
//	_, _, cancels, wg := setupNodeBlameForTest(c, partyNum)
//	defer cleanUp(c, cancels, wg, partyNum)
//
//	testNodeSyncBlame(c)
//
//	poolPubKey := testKeyGen(c, partyNum)
//	// we choose key sign to test blame because keysign have more rounds and easy for us to catch the stop point
//	testKeySignBlameTimeout(c, poolPubKey, cancels)
//}

func fabricateTssMsg(c *C, partyID *btss.PartyID, roundInfo, msg string) *p2p.WrappedMessage {
	routingInfo := btss.MessageRouting{
		From:                    partyID,
		To:                      nil,
		IsBroadcast:             true,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	wiredMessage := p2p.WireMessage{
		Routing:   &routingInfo,
		RoundInfo: roundInfo,
		Message:   []byte(msg),
	}
	marshaledMsg, err := json.Marshal(wiredMessage)
	c.Assert(err, IsNil)
	wrappedMsg := p2p.WrappedMessage{
		MessageType: p2p.TSSKeyGenMsg,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func fabricateVerMsg(c *C, hash, hashKey string) *p2p.WrappedMessage {
	broadcastConfirmMsg := &p2p.BroadcastConfirmMessage{
		P2PID: "",
		Key:   hashKey,
		Hash:  hash,
	}
	marshaledMsg, err := json.Marshal(broadcastConfirmMsg)
	c.Assert(err, IsNil)
	wrappedMsg := p2p.WrappedMessage{
		MessageType: p2p.TSSKeyGenVerMsg,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func senderIDtoPubKey(senderID *btss.PartyID) (string, error) {
	blamePartyKeyBytes := senderID.GetKey()
	var pk secp256k1.PubKeySecp256k1
	copy(pk[:], blamePartyKeyBytes)
	blamedPubKey, err := sdk.Bech32ifyAccPub(pk)
	return blamedPubKey, err
}

func (t *TssTestSuite) testVerMsgDuplication(c *C, tssCommonStruct *common.TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgDuplication"
	roundInfo := "round testVerMsgDuplication"
	msgHash, err := common.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, senderID.Id)
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)
	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedMsg = fabricateVerMsg(c, msgHash, msgKey)
	for i := 0; i < 2; i++ {
		err := tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
		c.Assert(err, IsNil)
		c.Assert(localItem.ConfirmedList, HasLen, 2)
	}
}

func setupProcessVerMsgEnv(c *C, keyPool []string, partyNum int) (*common.TssCommon, []*btss.PartyID, []*btss.PartyID) {

	common.ByPassGeneratePreParam = true
	sk, err := common.GetPriKey(testBlamePrivKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	conf := common.TssConfig{}
	keySignInstance := keysign.NewTssKeySign("", "", conf, sk, nil, nil, nil)
	tssCommonStruct := keySignInstance.GetTssCommonStruct()
	localTestPubKeys := make([]string, partyNum)
	copy(localTestPubKeys, keyPool[:partyNum])
	//for the test, we choose the first pubic key as the test instance public key
	partiesID, localPartyID, err := common.GetParties(localTestPubKeys, keyPool[0], true)
	c.Assert(err, IsNil)
	partyIDMap := common.SetupPartyIDMap(partiesID)
	common.SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 2)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan btsskeygen.LocalPartySaveData, len(partiesID))
	keyGenParty := btsskeygen.NewLocalParty(params, outCh, endCh)
	tssCommonStruct.SetPartyInfo(&common.PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	tssCommonStruct.SetLocalPeerID("fakeID")
	err = common.SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	peerPartiesID := append(partiesID[:localPartyID.Index], partiesID[localPartyID.Index+1:]...)
	tssCommonStruct.P2PPeers = common.GetPeersID(tssCommonStruct.PartyIDtoP2PID, tssCommonStruct.GetLocalPeerID())
	return tssCommonStruct, peerPartiesID, partiesID
}

func (t *TssTestSuite) testDropMsgOwner(c *C, tssCommonStruct *common.TssCommon, senderID *btss.PartyID, peerPartiesID []*btss.PartyID) {

	//clean up the blamepeer list for each test
	defer func() {
		tssCommonStruct.BlamePeers = common.NewBlame()
	}()
	testMsg := "testDropMsgOwner"
	roundInfo := "round testDropMsgOwner"
	msgHash, err := common.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	senderMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(senderMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	// -2 here as we exclude the last node (simulating that the data owner send the verMsg before the last node) and the
	// receiver itself.
	expected := len(peerPartiesID) - 1
	for i := 1; i < expected; i++ {
		err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[peerPartiesID[i].Id].String())
		c.Assert(err, IsNil)
		c.Assert(localItem.ConfirmedList, HasLen, i+1)
	}
	//the data owner's message should be raise an error
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, Equals, common.ErrHashFromOwner)
	c.Assert(tssCommonStruct.BlamePeers.FailReason, Equals, common.BlameHashCheck)
	blamedPubKey, err := senderIDtoPubKey(senderID)
	c.Assert(err, IsNil)
	c.Assert(tssCommonStruct.BlamePeers.BlameNodes, DeepEquals, []string{blamedPubKey})
}

func (t *TssTestSuite) testVerMsgAndUpdate(c *C, tssCommonStruct *common.TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {

	testMsg := "testVerMsgAndUpdate"
	roundInfo := "round testVerMsgAndUpdate"
	msgHash, err := common.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)
	//this error message indicates the message share is accepted by the this system.
	c.Assert(tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[2].Id].String()), ErrorMatches, "fail to update the message to local party: fail to set bytes to local party: task , party <nil>, round -1: proto: can't skip unknown wire type 4")
}

func (t *TssTestSuite) testVerMsgWrongHash(c *C, tssCommonStruct *common.TssCommon, senderID *btss.PartyID, peerParties []*btss.PartyID, testParties TestParties, senderMsg *p2p.WrappedMessage, peerMsgMap map[int]*p2p.WrappedMessage, msgKey string, blameOwner bool) {

	//clean up the blamepeer list for each test
	defer func() {
		tssCommonStruct.BlamePeers = common.NewBlame()
	}()

	err := tssCommonStruct.ProcessOneMessage(senderMsg, senderID.Id)
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	// we firstly load the host node message
	for i, nodeIndex := range testParties.honest {
		msg := peerMsgMap[nodeIndex]
		err = tssCommonStruct.ProcessOneMessage(msg, tssCommonStruct.PartyIDtoP2PID[peerParties[nodeIndex].Id].String())
		c.Assert(err, IsNil)
		c.Assert(localItem.ConfirmedList, HasLen, 2+i)
	}

	//we now load the malicious nodes message
	currentLength := len(localItem.ConfirmedList)
	for i, nodeIndex := range testParties.malicious {

		msg := peerMsgMap[nodeIndex]
		err = tssCommonStruct.ProcessOneMessage(msg, tssCommonStruct.PartyIDtoP2PID[peerParties[nodeIndex].Id].String())
		if err == nil {
			c.Assert(localItem.ConfirmedList, HasLen, currentLength+i+1)
			continue
		}
		c.Assert(err, ErrorMatches, "hashcheck error from peer")

		dataOwner, err := senderIDtoPubKey(senderID)
		c.Assert(err, IsNil)
		expected := []string{dataOwner}
		if blameOwner == false {
			for _, nodeIndex := range testParties.malicious {
				pubKey, err := senderIDtoPubKey(peerParties[nodeIndex])
				c.Assert(err, IsNil)
				expected = append(expected, pubKey)
			}
		}
		sort.Strings(tssCommonStruct.BlamePeers.BlameNodes)
		sort.Strings(expected)
		c.Assert(tssCommonStruct.BlamePeers.BlameNodes, DeepEquals, expected)
	}
}

// TestProcessVerMessage is the tests for processing the verified message
func (t *TssTestSuite) TestProcessVerMessage(c *C) {

	tssCommonStruct, peerPartiesID, partiesID := setupProcessVerMsgEnv(c, testBlamePubKeys, 4)
	t.testVerMsgDuplication(c, tssCommonStruct, partiesID[0], peerPartiesID)
	t.testVerMsgAndUpdate(c, tssCommonStruct, peerPartiesID[0], partiesID)
}

func constructMsg(c *C, senderID *btss.PartyID, testParties TestParties, modifiedHash []string, roundInfo, modifiedOwnerMsg string) (*p2p.WrappedMessage, map[int]*p2p.WrappedMessage, string) {
	wrappedMsgMap := make(map[int]*p2p.WrappedMessage)
	testMsg := "testVerMsgWrongHash"
	msgHash, err := common.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	senderMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	if len(modifiedOwnerMsg) != 0 {
		senderMsg = fabricateTssMsg(c, senderID, roundInfo, modifiedOwnerMsg)
	}

	//we send the verify message from the the same sender, Tss should only accept the first verify message

	for _, each := range testParties.honest {
		wrappedMsgMap[each] = fabricateVerMsg(c, msgHash, msgKey)
	}

	for i, each := range testParties.malicious {
		hash := modifiedHash[i]
		wrappedMsgMap[each] = fabricateVerMsg(c, hash, msgKey)
	}
	return senderMsg, wrappedMsgMap, msgKey
}

// TestProcessVerMessage is the tests for the hash inconsistency blame. Because of the complexity of the hash inconsistency check, we separate them from the TestProcessVerMessage. We simulate the hashchek is tested under the environment of 10 nodes
func (t *TssTestSuite) TestProcessVerMsgBlame(c *C) {

	tssCommonStruct, peerPartiesID, _ := setupProcessVerMsgEnv(c, testBlamePubKeys, 10)
	t.testDropMsgOwner(c, tssCommonStruct, peerPartiesID[0], peerPartiesID)
	// case 1, we test the blame that only peer report the wrong hash value and the msg owner
	testParties := TestParties{
		honest:    []int{1, 3, 4, 5, 6, 7, 8},
		malicious: []int{2},
	}
	//the modified hash should be paired with malicious nodes.
	modifiedHash := []string{"wrong"}
	roundInfo := "scenario1"
	senderMsg, wrappedMsgMap, msgKey := constructMsg(c, peerPartiesID[0], testParties, modifiedHash, roundInfo, "")
	t.testVerMsgWrongHash(c, tssCommonStruct, peerPartiesID[0], peerPartiesID, testParties, senderMsg, wrappedMsgMap, msgKey, false)

	// case 2, the msg owner send us the wrong msg, we just blame the msg owner
	testParties = TestParties{
		honest:    []int{1, 2, 3, 4, 5, 6, 7},
		malicious: []int{8},
	}
	//the modified hash should be paired with malicious nodes.
	roundInfo = "scenario2"
	testMsg := "testVerMsgWrongHash"
	msgHash, err := common.BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	modifiedHash = []string{msgHash}
	senderMsg, wrappedMsgMap, msgKey = constructMsg(c, peerPartiesID[0], testParties, modifiedHash, roundInfo, "aaa")
	t.testVerMsgWrongHash(c, tssCommonStruct, peerPartiesID[0], peerPartiesID, testParties, senderMsg, wrappedMsgMap, msgKey, true)

	// case 3, compared with the majority, there are two peers send different hash while these two nodes have the same hash
	testParties = TestParties{
		honest:    []int{1, 3, 4, 5, 6, 7},
		malicious: []int{2, 8},
	}
	//the modified hash should be paired with malicious nodes.
	roundInfo = "scenario3"
	modifiedHash = []string{"differentHash", "differentHash"}
	senderMsg, wrappedMsgMap, msgKey = constructMsg(c, peerPartiesID[0], testParties, modifiedHash, roundInfo, "")
	t.testVerMsgWrongHash(c, tssCommonStruct, peerPartiesID[0], peerPartiesID, testParties, senderMsg, wrappedMsgMap, msgKey, false)

	// case 4, compared with the majority, there are two peers send different hash while these two nodes have the same hash
	testParties = TestParties{
		honest:    []int{1, 5, 6, 7},
		malicious: []int{2, 3, 4, 8},
	}
	//the modified hash should be paired with malicious nodes.
	roundInfo = "scenario4"
	modifiedHash = []string{"differentHash", "differentHash", "differentHash2", "differentHash3"}
	senderMsg, wrappedMsgMap, msgKey = constructMsg(c, peerPartiesID[0], testParties, modifiedHash, roundInfo, "")
	t.testVerMsgWrongHash(c, tssCommonStruct, peerPartiesID[0], peerPartiesID, testParties, senderMsg, wrappedMsgMap, msgKey, false)

}
