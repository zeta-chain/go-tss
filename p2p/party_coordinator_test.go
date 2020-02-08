package p2p

import (
	"context"
	"math/rand"
	"sync"
	"testing"

	tnet "github.com/libp2p/go-libp2p-testing/net"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

func TestNewPartyCoordinator(t *testing.T) {
	applyDeadline = false
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
	pc1 := NewPartyCoordinator(h1)
	pc2 := NewPartyCoordinator(h2)
	pc3 := NewPartyCoordinator(h3)
	h1.SetStreamHandler(joinPartyProtocol, pc1.HandleStream)
	h2.SetStreamHandler(joinPartyProtocol, pc2.HandleStream)
	h3.SetStreamHandler(joinPartyProtocol, pc3.HandleStream)

	pc1.Start()
	pc2.Start()
	pc3.Start()

	msgID := RandStringBytesMask(64)
	joinPartyReq := messages.JoinPartyRequest{
		ID:        msgID,
		Threshold: 3,
		PeerID: []string{
			p1.String(), p2.String(), p3.String(),
		},
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc1.JoinParty(p1, &joinPartyReq)
		if err != nil {
			t.Error(err)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc2.JoinParty(p1, &joinPartyReq)
		if err != nil {
			t.Error(err)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc3.JoinParty(p1, &joinPartyReq)
		if err != nil {
			t.Error(err)
		}
		t.Log(resp)
	}()
	wg.Wait()
}

const letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ"
const (
	letterIdxBits = 6                    // 6 bits to represent a letter index
	letterIdxMask = 1<<letterIdxBits - 1 // All 1-bits, as many as letterIdxBits
	letterIdxMax  = 63 / letterIdxBits   // # of letter indices fitting in 63 bits
)

func RandStringBytesMask(n int) string {
	b := make([]byte, n)
	for i := 0; i < n; {
		if idx := int(rand.Int63() & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i++
		}
	}
	return string(b)
}
