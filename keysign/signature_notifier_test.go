package keysign

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"sync"
	"testing"
	"time"

	tsslibcommon "github.com/bnb-chain/tss-lib/common"
	tnet "github.com/libp2p/go-libp2p-testing/net"
	"github.com/libp2p/go-libp2p/core/peer"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/zeta-chain/go-tss/conversion"

	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/p2p"
)

func TestSignatureNotifierHappyPath(t *testing.T) {
	logger := zerolog.Nop()

	conversion.SetupBech32Prefix()
	poolPubKey := `thorpub1addwnpepq0ul3xt882a6nm6m7uhxj4tk2n82zyu647dyevcs5yumuadn4uamqx7neak`
	messageToSign := "yhEwrxWuNBGnPT/L7PNnVWg7gFWNzCYTV+GuX3tKRH8="
	buf, err := base64.StdEncoding.DecodeString(messageToSign)
	assert.Nil(t, err)
	messageID, err := common.MsgToHashString(buf)
	assert.Nil(t, err)
	p2p.ApplyDeadline.Store(false)
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New()
	// add peers to mock net

	a1 := tnet.RandLocalTCPAddress()
	a2 := tnet.RandLocalTCPAddress()
	a3 := tnet.RandLocalTCPAddress()

	h1, err := mn.AddPeer(id1.PrivateKey(), a1)
	if err != nil {
		t.Fatal(err)
	}
	p1 := h1.ID()
	h2, err := mn.AddPeer(id2.PrivateKey(), a2)
	if err != nil {
		t.Fatal(err)
	}
	p2 := h2.ID()
	h3, err := mn.AddPeer(id3.PrivateKey(), a3)
	if err != nil {
		t.Fatal(err)
	}
	p3 := h3.ID()
	if err := mn.LinkAll(); err != nil {
		t.Error(err)
	}
	if err := mn.ConnectAllButSelf(); err != nil {
		t.Error(err)
	}
	n1 := NewSignatureNotifier(h1, logger)
	n2 := NewSignatureNotifier(h2, logger)
	n3 := NewSignatureNotifier(h3, logger)
	assert.NotNil(t, n1)
	assert.NotNil(t, n2)
	assert.NotNil(t, n3)
	sigFile := "../test_data/signature_notify/sig1.json"
	content, err := os.ReadFile(sigFile)
	assert.Nil(t, err)
	assert.NotNil(t, content)
	var signature tsslibcommon.SignatureData
	err = json.Unmarshal(content, &signature)
	assert.Nil(t, err)
	sigChan := make(chan string)
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		sig, err := n1.WaitForSignature(messageID, [][]byte{buf}, poolPubKey, time.Second*30, sigChan)
		assert.Nil(t, err)
		assert.NotNil(t, sig)
	}()

	assert.Nil(t, n2.BroadcastSignature(messageID, []*tsslibcommon.SignatureData{&signature}, []peer.ID{
		p1, p3,
	}))
	assert.Nil(t, n3.BroadcastSignature(messageID, []*tsslibcommon.SignatureData{&signature}, []peer.ID{
		p1, p2,
	}))
	wg.Wait()
}

func TestSignatureNotifierBroadcastFirst(t *testing.T) {
	logger := zerolog.Nop()

	poolPubKey := `thorpub1addwnpepq0ul3xt882a6nm6m7uhxj4tk2n82zyu647dyevcs5yumuadn4uamqx7neak`
	messageToSign := "yhEwrxWuNBGnPT/L7PNnVWg7gFWNzCYTV+GuX3tKRH8="
	buf, err := base64.StdEncoding.DecodeString(messageToSign)
	assert.Nil(t, err)
	messageID, err := common.MsgToHashString(buf)
	assert.Nil(t, err)
	p2p.ApplyDeadline.Store(false)
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New()
	// add peers to mock net

	a1 := tnet.RandLocalTCPAddress()
	a2 := tnet.RandLocalTCPAddress()
	a3 := tnet.RandLocalTCPAddress()

	h1, err := mn.AddPeer(id1.PrivateKey(), a1)
	if err != nil {
		t.Fatal(err)
	}
	p1 := h1.ID()
	h2, err := mn.AddPeer(id2.PrivateKey(), a2)
	if err != nil {
		t.Fatal(err)
	}
	p2 := h2.ID()
	h3, err := mn.AddPeer(id3.PrivateKey(), a3)
	if err != nil {
		t.Fatal(err)
	}
	p3 := h3.ID()
	if err := mn.LinkAll(); err != nil {
		t.Error(err)
	}
	if err := mn.ConnectAllButSelf(); err != nil {
		t.Error(err)
	}
	n1 := NewSignatureNotifier(h1, logger)
	n2 := NewSignatureNotifier(h2, logger)
	n3 := NewSignatureNotifier(h3, logger)
	assert.NotNil(t, n1)
	assert.NotNil(t, n2)
	assert.NotNil(t, n3)
	sigFile := "../test_data/signature_notify/sig1.json"
	content, err := os.ReadFile(sigFile)
	assert.Nil(t, err)
	assert.NotNil(t, content)
	var signature tsslibcommon.SignatureData
	err = json.Unmarshal(content, &signature)
	assert.Nil(t, err)
	sigChan := make(chan string)

	assert.NotContains(t, n1.notifiers, messageID)

	assert.Nil(t, n2.BroadcastSignature(messageID, []*tsslibcommon.SignatureData{&signature}, []peer.ID{
		p1, p3,
	}))

	assert.Nil(t, n3.BroadcastSignature(messageID, []*tsslibcommon.SignatureData{&signature}, []peer.ID{
		p1, p2,
	}))

	n1.notifierLock.Lock()
	require.Contains(t, n1.notifiers, messageID)
	notifier := n1.notifiers[messageID]
	n1.notifierLock.Unlock()
	assert.False(t, notifier.readyToProcess())
	assert.Equal(t, defaultNotifierTTL, notifier.ttl)

	sig, err := n1.WaitForSignature(messageID, [][]byte{buf}, poolPubKey, time.Second*30, sigChan)
	assert.Nil(t, err)
	assert.NotNil(t, sig)

	n1.notifierLock.Lock()
	assert.NotContains(t, n1.notifiers, messageID)
	n1.notifierLock.Unlock()

	// check ttl logic and cleanup
	n3.notifierLock.Lock()
	assert.Contains(t, n3.notifiers, messageID)
	notifier = n3.notifiers[messageID]
	notifier.ttl = 0
	n3.notifierLock.Unlock()

	n3.Start()
	defer n3.Stop()

	// let cleanup goroutine run
	time.Sleep(time.Second)

	n3.notifierLock.Lock()
	assert.NotContains(t, n3.notifiers, messageID)
	n3.notifierLock.Unlock()
}
