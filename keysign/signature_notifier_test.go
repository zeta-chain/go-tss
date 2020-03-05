package keysign

import (
	"context"
	"testing"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/signing"
	"github.com/libp2p/go-libp2p-core/peer"
	tnet "github.com/libp2p/go-libp2p-testing/net"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"

	"gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

func TestSignatureNotifierHappyPath(t *testing.T) {
	p2p.ApplyDeadline = false
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New(context.Background())
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
	n1 := NewSignatureNotifier(h1)
	n2 := NewSignatureNotifier(h2)
	n3 := NewSignatureNotifier(h3)
	assert.NotNil(t, n1)
	assert.NotNil(t, n2)
	assert.NotNil(t, n3)
	assert.Nil(t, n1.Start())
	assert.Nil(t, n2.Start())
	assert.Nil(t, n3.Start())
	s := &signing.SignatureData{
		Signature:         []byte(go_tss.RandStringBytesMask(32)),
		SignatureRecovery: []byte(go_tss.RandStringBytesMask(32)),
		R:                 []byte(go_tss.RandStringBytesMask(32)),
		S:                 []byte(go_tss.RandStringBytesMask(32)),
		M:                 []byte(go_tss.RandStringBytesMask(32)),
	}
	messageID := go_tss.RandStringBytesMask(24)
	go func() {
		sig, err := n1.WaitForSignature(messageID, []peer.ID{
			p2, p3,
		}, time.Second*30)
		assert.Nil(t, err)
		assert.NotNil(t, sig)
	}()
	assert.Nil(t, n2.BroadcastSignature(messageID, s, []peer.ID{
		p1, p3,
	}))
	assert.Nil(t, n3.BroadcastSignature(messageID, s, []peer.ID{
		p1, p2,
	}))
}
