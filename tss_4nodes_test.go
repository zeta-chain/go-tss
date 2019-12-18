package tss

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
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

var testPubKeys = [...]string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}
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

func spinUpServers(c *C, localTss []*Tss, ctxs []context.Context, wg sync.WaitGroup, partyNum int) {
	//we spin up the first signer as the "bootstrap node", and the rest 3 nodes connect to it
	startServerAndCheck(c, wg, localTss[0], ctxs[0], baseTssPort)
	for i := 1; i < partyNum; i++ {
		startServerAndCheck(c, wg, localTss[i], ctxs[i], baseTssPort+i)
	}
}

func setupContextAndNodes(c *C, partyNum int) ([]context.Context, []context.CancelFunc, []*Tss) {
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

func setupNodeForTest(c *C, partyNum int) ([]context.Context, []*Tss, []context.CancelFunc, *sync.WaitGroup) {

	ctxs, cancels, localTss := setupContextAndNodes(c, partyNum)
	wg := sync.WaitGroup{}
	spinUpServers(c, localTss, ctxs, wg, partyNum)

	return ctxs, localTss, cancels, &wg
}

func sendTestRequest(c *C, url string, request []byte) []byte {
	resp, err := http.Post(url, "application/json", bytes.NewBuffer(request))
	c.Assert(err, IsNil)
	body, err := ioutil.ReadAll(resp.Body)
	c.Assert(err, IsNil)
	return body

}

func testKeySign(c *C, poolPubKey string, partyNum int) {
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
	c.Assert(keySignRespArr[1].S, HasLen, 44)
	for i := 1; i < partyNum-1; i++ {
		c.Assert(keySignRespArr[i].S, Equals, keySignRespArr[i+1].S)
		c.Assert(keySignRespArr[i].R, Equals, keySignRespArr[i+1].R)
	}
}

func testKeyGen(c *C, partyNum int) string {
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

func cleanUp(c *C, cancels []context.CancelFunc, wg *sync.WaitGroup, partyNum int) {
	for i := 0; i < partyNum; i++ {
		cancels[i]()
		directoryPath := path.Join(testFileLocation, strconv.Itoa(i))
		err := os.RemoveAll(directoryPath)
		c.Assert(err, IsNil)
	}
	wg.Wait()
}

func (t *TssTestSuite) Test4NodesTss(c *C) {
	_, _, cancels, wg := setupNodeForTest(c, partyNum)
	defer cleanUp(c, cancels, wg, partyNum)
	//test key gen.
	testKeyGen(c, partyNum)
	//test key sign.
	//testKeySign(c, poolPubKey, partyNum)
	//test the message process channel
	//t.testTssProcessOutCh(c, tssNodes[0])
}

func (t *TssTestSuite) testTssProcessOutCh(c *C, tssNode *Tss) {
	_, localPartyID, err := tssNode.getParties(testPubKeys[:], testPubKeys[0], true)
	c.Assert(err, IsNil)
	messageRouting := btss.MessageRouting{
		From:                    localPartyID,
		To:                      nil,
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
	err = tssNode.processOutCh(tssMsg, TSSKeyGenMsg)
	fmt.Println(err)
	c.Assert(err, IsNil)
}

func fabricateTssMsg(c *C, partyID *btss.PartyID, roundInfo, msg string) *WrappedMessage {
	routingInfo := btss.MessageRouting{
		From:                    partyID,
		To:                      nil,
		IsBroadcast:             true,
		IsToOldCommittee:        false,
		IsToOldAndNewCommittees: false,
	}
	wiredMessage := WireMessage{
		Routing:   &routingInfo,
		RoundInfo: roundInfo,
		Message:   []byte(msg),
	}
	marshaledMsg, err := json.Marshal(wiredMessage)
	c.Assert(err, IsNil)
	wrappedMsg := WrappedMessage{
		MessageType: TSSKeyGenMsg,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func fabricateVerMsg(c *C, hash, hashKey string) *WrappedMessage {
	broadcastConfirmMsg := &BroadcastConfirmMessage{
		P2PID: "",
		Key:   hashKey,
		Hash:  hash,
	}
	marshaledMsg, err := json.Marshal(broadcastConfirmMsg)
	c.Assert(err, IsNil)
	wrappedMsg := WrappedMessage{
		MessageType: TSSKeyGenVerMsg,
		Payload:     marshaledMsg,
	}
	return &wrappedMsg
}

func (t *TssTestSuite) testVerMsgDuplication(c *C, tssNode *Tss, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgDuplication"
	roundInfo := "round testVerMsgDuplication"
	msgHash, err := bytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssNode.processOneMessage(wrappedMsg, senderID.Id)
	c.Assert(err, IsNil)
	localItem := tssNode.tryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedMsg = fabricateVerMsg(c, msgHash, msgKey)
	for i := 0; i < 2; i++ {
		err := tssNode.processOneMessage(wrappedMsg, tssNode.partyIDtoP2PID[partiesID[1].Id].String())
		c.Assert(err, IsNil)
		c.Assert(localItem.ConfirmedList, HasLen, 2)
	}
}

func (t *TssTestSuite) testDropMsgOwner(c *C, tssNode *Tss, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testDropMsgOwner"
	roundInfo := "round testDropMsgOwner"
	msgHash, err := bytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssNode.processOneMessage(wrappedMsg, tssNode.partyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssNode.tryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssNode.processOneMessage(wrappedVerMsg, tssNode.partyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)

	//the data owner's message should be dropped
	err = tssNode.processOneMessage(wrappedVerMsg, tssNode.partyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)
}

func (t *TssTestSuite) testVerMsgAndUpdate(c *C, tssNode *Tss, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgAndUpdate"
	roundInfo := "round testVerMsgAndUpdate"
	msgHash, err := bytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssNode.processOneMessage(wrappedMsg, tssNode.partyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssNode.tryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssNode.processOneMessage(wrappedVerMsg, tssNode.partyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)
	//this panic indicates the message share is accepted by the this system.
	c.Assert(func() { tssNode.processOneMessage(wrappedVerMsg, tssNode.partyIDtoP2PID[partiesID[2].Id].String()) }, PanicMatches, `runtime error: invalid memory address or nil pointer dereference`)
}

func (t *TssTestSuite) testVerMsgWrongHash(c *C, tssNode *Tss, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgWrongHash"
	roundInfo := "round testVerMsgWrongHash"
	msgHash, err := bytesToHashString([]byte(testMsg))
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	err = tssNode.processOneMessage(wrappedMsg, senderID.Id)
	c.Assert(err, IsNil)
	localItem := tssNode.tryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedMsg = fabricateVerMsg(c, msgHash, msgKey)
	err = tssNode.processOneMessage(wrappedMsg, tssNode.partyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)

	msgHash2 := "a" + msgHash
	wrappedMsg = fabricateVerMsg(c, msgHash2, msgKey)
	err = tssNode.processOneMessage(wrappedMsg, tssNode.partyIDtoP2PID[partiesID[2].Id].String())
	c.Assert(localItem.ConfirmedList, HasLen, 3)
	c.Assert(err, ErrorMatches, "hash is not in consistency")
}

func (t *TssTestSuite) TestProcessVerMessage(c *C) {
	_, tssNodes, cancels, wg := setupNodeForTest(c, 1)
	defer cleanUp(c, cancels, wg, 1)
	tssNode := tssNodes[0]
	partiesID, localPartyID, err := tssNode.getParties(testPubKeys[:], testPubKeys[0], true)
	partyIDMap := setupPartyIDMap(partiesID)
	setupIDMaps(partyIDMap, tssNode.partyIDtoP2PID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 2)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan keygen.LocalPartySaveData, len(partiesID))
	keyGenParty := keygen.NewLocalParty(params, outCh, endCh)
	tssNode.setPartyInfo(&PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	err = setupIDMaps(partyIDMap, tssNode.partyIDtoP2PID)
	c.Assert(err, IsNil)
	partiesID = append(partiesID[:localPartyID.Index], partiesID[localPartyID.Index+1:]...)

	t.testVerMsgDuplication(c, tssNode, partiesID[0], partiesID)
	t.testVerMsgWrongHash(c, tssNode, partiesID[0], partiesID)
	t.testVerMsgAndUpdate(c, tssNode, partiesID[0], partiesID)
	t.testDropMsgOwner(c, tssNode, partiesID[0], partiesID)
}
