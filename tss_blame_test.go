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

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

type BlameTestSuite struct{}

var _ = Suite(&BlameTestSuite{})

// we have these 10 fake public keys here as we simulate to test hash check happen in 10 nodes environment.
var testBlamePubKeys = []string{"thorpub1addwnpepqtr5p8tllhp4xaxmu77zhqen24pmrdlnekzevshaqkyzdqljm6rejnnt02t", "thorpub1addwnpepqwz59mn4ae82svm2pfqnycducjjeez0qlf3sum7rclrr8mn44pr5gkeey25", "thorpub1addwnpepqga4nded5hhnwsrwmrns803w7vu9mffp9r6dz4l6smaww2l5useuq6vkttg", "thorpub1addwnpepq28hfdpu3rdgvj8skzhlm8hyt5nlwwc8pjrzvn253j86e4dujj6jsmuf25q", "thorpub1addwnpepqfuq0xc67052h288r6flp67l0ny9mg6u3sxhsrlukyfg0fe9j6q36ysd33y", "thorpub1addwnpepq0jszts80udfl4pkfk6cp93647yl6fhu6pk486uwjdz2sf94qvu0kw0t6ug", "thorpub1addwnpepqw6mmffk69n5taaqhq3wsc8mvdpsrdnx960kujeh4jwm9lj8nuyux9hz5e4", "thorpub1addwnpepq0pdhm2jatzg2vy6fyw89vs6q374zayqd5498wn8ww780grq256ygq7hhjt", "thorpub1addwnpepqggwmlgd8u9t2sx4a0styqwhzrvdhpvdww7sqwnweyrh25rjwwm9q65kx9s", "thorpub1addwnpepqtssltyjvms8pa7k4yg85lnrjqtvvr2ecr36rhm7pa4ztf55tnuzzgvegpk"}

var testBlamePrivKey = "OWU2YTk1NzdlOTA5NTAxZmI4YjUyODYyMmZkYzBjNzJlMTQ5YTI2YWY5NzkzYTc0MjA3MDBkMWQzMzFiMDNhZg=="

func (t *BlameTestSuite) SetUpSuite(c *C) {
	common.InitLog("info", true)
}

type TestParties struct {
	honest    []int
	malicious []int
}

func setupNodeBlameForTest(c *C, partyNum int) ([]context.Context, []*TssServer, []context.CancelFunc, *sync.WaitGroup) {
	conf := common.TssConfig{
		KeyGenTimeout:   time.Second * 5,
		KeySignTimeout:  time.Second * 5,
		SyncTimeout:     time.Second * 2,
		PreParamTimeout: time.Second * 5,
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
	c.Assert(blames, DeepEquals, targetBlamePeers)
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

func (t *BlameTestSuite) TestNodeSyncAndTimeoutBlame(c *C) {
	ByPassGeneratePreParam = false
	_, _, cancels, wg := setupNodeBlameForTest(c, partyNum)
	defer cleanUp(c, cancels, wg, partyNum)

	testNodeSyncBlame(c)

	poolPubKey := testKeyGen(c, partyNum)
	// we choose key sign to test blame because keysign have more rounds and easy for us to catch the stop point
	testKeySignBlameTimeout(c, poolPubKey, cancels)
}
