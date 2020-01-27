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
	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/thornode/cmd"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const testPriKey = "OTI4OTdkYzFjMWFhMjU3MDNiMTE4MDM1OTQyY2Y3MDVkOWFhOGIzN2JlOGIwOWIwMTZjYTkxZjNjOTBhYjhlYQ=="

var testPubKeys = [...]string{"thorpub1addwnpepqtdklw8tf3anjz7nn5fly3uvq2e67w2apn560s4smmrt9e3x52nt2svmmu3", "thorpub1addwnpepqtspqyy6gk22u37ztra4hq3hdakc0w0k60sfy849mlml2vrpfr0wvm6uz09", "thorpub1addwnpepq2ryyje5zr09lq7gqptjwnxqsy2vcdngvwd6z7yt5yjcnyj8c8cn559xe69", "thorpub1addwnpepqfjcw5l4ay5t00c32mmlky7qrppepxzdlkcwfs2fd5u73qrwna0vzag3y4j"}

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	initLog("info", true)
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

func (t *TssTestSuite) testDropMsgOwner(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testDropMsgOwner"
	roundInfo := "round testDropMsgOwner"
	msgHash, err := BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	// you can pass any p2pID in Tss message
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	wrappedVerMsg := fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)

	//the data owner's message should be dropped
	err = tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[senderID.Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)
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
	//this panic indicates the message share is accepted by the this system.
	c.Assert(tssCommonStruct.ProcessOneMessage(wrappedVerMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[2].Id].String()), ErrorMatches, "fail to update the message to local party: fail to set bytes to local party: task , party <nil>, round -1: proto: can't skip unknown wire type 4")
}

func (t *TssTestSuite) testVerMsgWrongHash(c *C, tssCommonStruct *TssCommon, senderID *btss.PartyID, partiesID []*btss.PartyID) {
	testMsg := "testVerMsgWrongHash"
	roundInfo := "round testVerMsgWrongHash"
	msgHash, err := BytesToHashString([]byte(testMsg))
	c.Assert(err, IsNil)
	msgKey := fmt.Sprintf("%s-%s", senderID.Id, roundInfo)
	wrappedMsg := fabricateTssMsg(c, senderID, roundInfo, testMsg)
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, senderID.Id)
	c.Assert(err, IsNil)
	localItem := tssCommonStruct.TryGetLocalCacheItem(msgKey)
	c.Assert(localItem.ConfirmedList, HasLen, 1)

	//we send the verify message from the the same sender, Tss should only accept the first verify message
	wrappedMsg = fabricateVerMsg(c, msgHash, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[1].Id].String())
	c.Assert(err, IsNil)
	c.Assert(localItem.ConfirmedList, HasLen, 2)

	msgHash2 := "a" + msgHash
	wrappedMsg = fabricateVerMsg(c, msgHash2, msgKey)
	err = tssCommonStruct.ProcessOneMessage(wrappedMsg, tssCommonStruct.PartyIDtoP2PID[partiesID[2].Id].String())
	c.Assert(localItem.ConfirmedList, HasLen, 3)
	c.Assert(err, ErrorMatches, "hash is not in consistency")
}

func (t *TssTestSuite) TestProcessVerMessage(c *C) {

	sk, err := GetPriKey(testPriKey)
	c.Assert(err, IsNil)
	c.Assert(sk, NotNil)
	conf := TssConfig{}

	tssCommonStruct := NewTssCommon("",nil, conf)
	localTestPubKeys := make([]string, len(testPubKeys))
	copy(localTestPubKeys, testPubKeys[:])

	partiesID, localPartyID, err := GetParties(localTestPubKeys, testPubKeys[0], true)
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
	err = SetupIDMaps(partyIDMap, tssCommonStruct.PartyIDtoP2PID)
	c.Assert(err, IsNil)
	peerPartiesID := append(partiesID[:localPartyID.Index], partiesID[localPartyID.Index+1:]...)
	tssCommonStruct.P2PPeers = GetPeersID(tssCommonStruct.PartyIDtoP2PID, tssCommonStruct.GetLocalPeerID())

	t.testVerMsgDuplication(c, tssCommonStruct, partiesID[0], peerPartiesID)
	t.testVerMsgWrongHash(c, tssCommonStruct, peerPartiesID[0], peerPartiesID)
	t.testVerMsgAndUpdate(c, tssCommonStruct, peerPartiesID[0], partiesID)
	t.testDropMsgOwner(c, tssCommonStruct, peerPartiesID[0], partiesID)
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
	tssCommonStruct := NewTssCommon("",nil, conf)
	err = tssCommonStruct.ProcessOutCh(tssMsg, p2p.TSSKeyGenMsg)
	c.Assert(err, IsNil)
}

