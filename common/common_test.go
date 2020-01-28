package common

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sort"
	"testing"

	btsskeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/thornode/cmd"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

var testBlamePrivKey = "OWU2YTk1NzdlOTA5NTAxZmI4YjUyODYyMmZkYzBjNzJlMTQ5YTI2YWY5NzkzYTc0MjA3MDBkMWQzMzFiMDNhZg=="
var testPubKeys = [...]string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}
var testBlamePubKeys = []string{"thorpub1addwnpepqtr5p8tllhp4xaxmu77zhqen24pmrdlnekzevshaqkyzdqljm6rejnnt02t", "thorpub1addwnpepqwz59mn4ae82svm2pfqnycducjjeez0qlf3sum7rclrr8mn44pr5gkeey25", "thorpub1addwnpepqga4nded5hhnwsrwmrns803w7vu9mffp9r6dz4l6smaww2l5useuq6vkttg", "thorpub1addwnpepq28hfdpu3rdgvj8skzhlm8hyt5nlwwc8pjrzvn253j86e4dujj6jsmuf25q", "thorpub1addwnpepqfuq0xc67052h288r6flp67l0ny9mg6u3sxhsrlukyfg0fe9j6q36ysd33y", "thorpub1addwnpepq0jszts80udfl4pkfk6cp93647yl6fhu6pk486uwjdz2sf94qvu0kw0t6ug", "thorpub1addwnpepqw6mmffk69n5taaqhq3wsc8mvdpsrdnx960kujeh4jwm9lj8nuyux9hz5e4", "thorpub1addwnpepq0pdhm2jatzg2vy6fyw89vs6q374zayqd5498wn8ww780grq256ygq7hhjt", "thorpub1addwnpepqggwmlgd8u9t2sx4a0styqwhzrvdhpvdww7sqwnweyrh25rjwwm9q65kx9s", "thorpub1addwnpepqtssltyjvms8pa7k4yg85lnrjqtvvr2ecr36rhm7pa4ztf55tnuzzgvegpk"}

type TestParties struct {
	honest    []int
	malicious []int
}

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	InitLog("info", true)
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(cmd.Bech32PrefixAccAddr, cmd.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(cmd.Bech32PrefixValAddr, cmd.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(cmd.Bech32PrefixConsAddr, cmd.Bech32PrefixConsPub)
}

func initLog(level string, pretty bool) {
	l, err := zerolog.ParseLevel(level)
	if err != nil {
		log.Warn().Msgf("%s is not a valid log-level, falling back to 'info'", level)
	}
	var out io.Writer = os.Stdout
	if pretty {
		out = zerolog.ConsoleWriter{Out: os.Stdout}
	}
	zerolog.SetGlobalLevel(l)
	log.Logger = log.Output(out).With().Str("service", "go-tss-test").Logger()
}

func (t *TssTestSuite) TestGetThreshold(c *C) {
	_, err := GetThreshold(-2)
	c.Assert(err, NotNil)
	output, err := GetThreshold(4)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 2)
	output, err = GetThreshold(9)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 5)
	output, err = GetThreshold(10)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 6)
	output, err = GetThreshold(99)
	c.Assert(err, IsNil)
	c.Assert(output, Equals, 65)
}

func (t *TssTestSuite) TestMsgToHashInt(c *C) {
	input := []byte("whatever")
	result, err := MsgToHashInt(input)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
}

func (t *TssTestSuite) TestContains(c *C) {
	t1 := btss.PartyID{
		Index: 1,
	}
	ret := Contains(nil, &t1)
	c.Assert(ret, Equals, false)

	t2 := btss.PartyID{
		Index: 2,
	}
	t3 := btss.PartyID{
		Index: 3,
	}
	testParties := []*btss.PartyID{&t2, &t3}
	ret = Contains(testParties, &t1)
	c.Assert(ret, Equals, false)
	testParties = append(testParties, &t1)
	ret = Contains(testParties, &t1)
	c.Assert(ret, Equals, true)
	ret = Contains(testParties, nil)
	c.Assert(ret, Equals, false)
}

func doNodeSyncTest(c *C, peers []peer.ID, targets []peer.ID) {
	tssCommonStruct := TssCommon{
		PartyIDtoP2PID: nil,
		TssMsg:         nil,
		P2PPeers:       peers,
	}
	receiver := make(chan *p2p.Message, 4)
	var expected []string
	for _, each := range targets {
		msg := p2p.Message{
			PeerID:  each,
			Payload: []byte("hello"),
		}
		receiver <- &msg
		expected = append(expected, each.String())
	}
	returnedPeers, err := tssCommonStruct.NodeSync(receiver, p2p.TSSKeySignVerMsg)
	sort.Strings(returnedPeers)
	sort.Strings(expected)
	c.Assert(returnedPeers, DeepEquals, expected)
	if len(peers) == len(targets) {
		c.Assert(err, IsNil)
	} else {
		c.Assert(err, NotNil)
	}
}

func (t *TssTestSuite) TestNodeSync(c *C) {
	//in test, we pretend to be node1
	node2, err := peer.IDB58Decode("16Uiu2HAmAWKWf5vnpiAhfdSQebTbbB3Bg35qtyG7Hr4ce23VFA8V")
	c.Assert(err, IsNil)
	node3, err := peer.IDB58Decode("16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh")
	c.Assert(err, IsNil)
	node4, err := peer.IDB58Decode("16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa")
	c.Assert(err, IsNil)

	peers := []peer.ID{node2, node3, node4}
	target := []peer.ID{node4}
	doNodeSyncTest(c, peers, target)
	target = []peer.ID{node2, node4}
	doNodeSyncTest(c, peers, target)
	// in the 4 nodes scenario, we need to have the response of 3 nodes(loopback msg is not broadcast in the p2p)
	target = []peer.ID{node2, node3, node4}
	doNodeSyncTest(c, peers, target)
}

func (t *TssTestSuite) TestGetPriKey(c *C) {
	pk, err := GetPriKey("whatever")
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	input := base64.StdEncoding.EncodeToString([]byte("whatever"))
	pk, err = GetPriKey(input)
	c.Assert(err, NotNil)
	c.Assert(pk, IsNil)
	//pk, err = GetPriKey("MmVhNTI1ZDk3N2Y1NWU3OWM3M2JhNjZiNzM2NDU0ZGI2Mjc2NmU4ZTMzMzg2ZDlhZGM4YmI2MjE2NmRiMWFkMQ==")
	pk, err = GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(pk, NotNil)
	result, err := GetPriKeyRawBytes(pk)
	c.Assert(err, IsNil)
	c.Assert(result, NotNil)
	c.Assert(result, HasLen, 32)
}

func (t *TssTestSuite) TestTssProcessOutCh(c *C) {
	sk, err := GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	conf := TssConfig{}
	localTestPubKeys := make([]string, len(testPubKeys))
	copy(localTestPubKeys, testPubKeys[:])
	partiesID, localPartyID, err := GetParties(localTestPubKeys, testPubKeys[0], true)
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
	tssCommonStruct := NewTssCommon("", nil, conf)
	err = tssCommonStruct.ProcessOutCh(tssMsg, p2p.TSSKeyGenMsg)
	c.Assert(err, IsNil)
}

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

func (t *TssTestSuite) testVerMsgDuplication(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgDuplication"
	roundInfo := "round testVerMsgDuplication"
	msgHash, err := BytesToHashString([]byte(testMsg))
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

func setupProcessVerMsgEnv(c *C, keyPool []string, partyNum int) (*TssCommon, []*btss.PartyID, []*btss.PartyID) {

	ByPassGeneratePreParam = true
	sk, err := GetPriKey(testBlamePrivKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	conf := TssConfig{}
	//keySignInstance := keysign.NewTssKeySign("", "", conf, sk, nil, nil, nil)
	tssCommonStruct := NewTssCommon("", nil, conf)
	//tssCommonStruct := keySignInstance.GetTssCommonStruct()
	localTestPubKeys := make([]string, partyNum)
	copy(localTestPubKeys, keyPool[:partyNum])
	//for the test, we choose the first pubic key as the test instance public key
	partiesID, localPartyID, err := GetParties(localTestPubKeys, keyPool[0], true)
	c.Assert(err, IsNil)
	partyIDMap := SetupPartyIDMap(partiesID)
	SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	ctx := btss.NewPeerContext(partiesID)
	params := btss.NewParameters(ctx, localPartyID, len(partiesID), 2)
	outCh := make(chan btss.Message, len(partiesID))
	endCh := make(chan btsskeygen.LocalPartySaveData, len(partiesID))
	keyGenParty := btsskeygen.NewLocalParty(params, outCh, endCh)
	tssCommonStruct.SetPartyInfo(&PartyInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	})
	tssCommonStruct.SetLocalPeerID("fakeID")
	err = SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	peerPartiesID := append(partiesID[:localPartyID.Index], partiesID[localPartyID.Index+1:]...)
	tssCommonStruct.P2PPeers = GetPeersID(tssCommonStruct.PartyIDtoP2PID, tssCommonStruct.GetLocalPeerID())
	return tssCommonStruct, peerPartiesID, partiesID
}

func (t *TssTestSuite) testDropMsgOwner(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, peerPartiesID []*btss.PartyID) {

	//clean up the blamepeer list for each test
	defer func() {
		tssCommonStruct.BlamePeers = NewBlame()
	}()
	testMsg := "testDropMsgOwner"
	roundInfo := "round testDropMsgOwner"
	msgHash, err := BytesToHashString([]byte(testMsg))
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
	c.Assert(err, Equals, ErrHashFromOwner)
	c.Assert(tssCommonStruct.BlamePeers.FailReason, Equals, BlameHashCheck)
	blamedPubKey, err := senderIDtoPubKey(senderID)
	c.Assert(err, IsNil)
	c.Assert(tssCommonStruct.BlamePeers.BlameNodes, DeepEquals, []string{blamedPubKey})
}

func (t *TssTestSuite) testVerMsgAndUpdate(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {

	testMsg := "testVerMsgAndUpdate"
	roundInfo := "round testVerMsgAndUpdate"
	msgHash, err := BytesToHashString([]byte(testMsg))
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

func (t *TssTestSuite) testVerMsgWrongHash(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, peerParties []*btss.PartyID, testParties TestParties, senderMsg *p2p.WrappedMessage, peerMsgMap map[int]*p2p.WrappedMessage, msgKey string, blameOwner bool) {

	//clean up the blamepeer list for each test
	defer func() {
		tssCommonStruct.BlamePeers = NewBlame()
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
	msgHash, err := BytesToHashString([]byte(testMsg))
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
	msgHash, err := BytesToHashString([]byte(testMsg))
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
