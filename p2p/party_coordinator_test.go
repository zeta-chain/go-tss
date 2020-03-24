package p2p

import (
	"context"
	"sync"
	"testing"
	"time"

	tnet "github.com/libp2p/go-libp2p-testing/net"
	mocknet "github.com/libp2p/go-libp2p/p2p/net/mock"
	"github.com/stretchr/testify/assert"

	"gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/messages"
)

func TestNewPartyCoordinator(t *testing.T) {
	ApplyDeadline = false
	id1 := tnet.RandIdentityOrFatal(t)
	id2 := tnet.RandIdentityOrFatal(t)
	id3 := tnet.RandIdentityOrFatal(t)
	id4 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New(context.Background())
	// add peers to mock net

	a1 := tnet.RandLocalTCPAddress()
	a2 := tnet.RandLocalTCPAddress()
	a3 := tnet.RandLocalTCPAddress()
	a4 := tnet.RandLocalTCPAddress()

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
	h4, err := mn.AddPeer(id4.PrivateKey(), a4)
	if err != nil {
		t.Fatal(err)
	}
	p4 := h4.ID()
	if err := mn.LinkAll(); err != nil {
		t.Error(err)
	}
	if err := mn.ConnectAllButSelf(); err != nil {
		t.Error(err)
	}
	timeout := time.Second * 5
	pc1 := NewPartyCoordinator(h1, timeout)
	pc2 := NewPartyCoordinator(h2, timeout)
	pc3 := NewPartyCoordinator(h3, timeout)

	defer pc1.Stop()
	defer pc2.Stop()
	defer pc3.Stop()

	msgID := go_tss.RandStringBytesMask(64)
	threshold := int32(3)
	peers := []string{
		p1.String(), p2.String(), p3.String(),
	}
	joinPartyReq := messages.JoinPartyRequest{
		ID: msgID,
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc1.JoinParty(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc2.JoinParty(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc3.JoinParty(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}

		t.Log(resp)
	}()
	wg.Wait()

	msgID1 := go_tss.RandStringBytesMask(64)
	threshold = int32(3)
	peers = []string{
		p1.String(), p2.String(), p4.String(),
	}
	joinPartyReq1 := messages.JoinPartyRequest{
		ID: msgID1,
	}
	wg = sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc1.JoinParty(p1, &joinPartyReq1, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_Timeout {
			t.Errorf("expect response type to be timeout , however we got:%s", resp.Type)
		}

		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc2.JoinParty(p1, &joinPartyReq1, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_Timeout {
			t.Errorf("expect response type to be timeout , however we got:%s", resp.Type)
		}

		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc3.JoinParty(p1, &joinPartyReq1, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_UnknownPeer {
			t.Errorf("expect unknown peer, however we got:%s", resp.Type)
		}
		t.Log(resp)
	}()
	wg.Wait()
	// if peer is not party of the party it should fail
}

func TestNewPartyCoordinatorWithTimeout(t *testing.T) {
	ApplyDeadline = false
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
	timeout := time.Second * 5
	pc1 := NewPartyCoordinator(h1, timeout)
	pc2 := NewPartyCoordinator(h2, timeout)
	pc3 := NewPartyCoordinator(h3, timeout)

	defer pc1.Stop()
	defer pc2.Stop()
	defer pc3.Stop()

	msgID := go_tss.RandStringBytesMask(64)
	threshold := int32(4)
	peers := []string{p1.String(), p2.String(), p3.String()}
	joinPartyReq := messages.JoinPartyRequest{
		ID: msgID,
	}
	wg := sync.WaitGroup{}
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc1.JoinPartyWithRetry(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_Timeout {
			t.Errorf("expect response type to be timeout , however we got:%s", resp.Type)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc2.JoinPartyWithRetry(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_Timeout {
			t.Errorf("expect response type to be timeout , however we got:%s", resp.Type)
		}
		t.Log(resp)
	}()
	wg.Add(1)
	go func() {
		defer wg.Done()
		resp, err := pc3.JoinPartyWithRetry(p1, &joinPartyReq, peers, threshold)
		if err != nil {
			t.Error(err)
		}
		if resp.Type != messages.JoinPartyResponse_Timeout {
			t.Errorf("expect response type to be timeout , however we got:%s", resp.Type)
		}
		t.Log(resp)
	}()
	wg.Wait()
}

func TestGetPeerIDs(t *testing.T) {
	ApplyDeadline = false
	id1 := tnet.RandIdentityOrFatal(t)
	mn := mocknet.New(context.Background())
	// add peers to mock net

	a1 := tnet.RandLocalTCPAddress()
	h1, err := mn.AddPeer(id1.PrivateKey(), a1)
	if err != nil {
		t.Fatal(err)
	}
	p1 := h1.ID()
	timeout := time.Second * 5
	pc := NewPartyCoordinator(h1, timeout)
	r, err := pc.getPeerIDs([]string{})
	assert.Nil(t, err)
	assert.Len(t, r, 0)
	input := []string{
		p1.String(),
	}
	r1, err := pc.getPeerIDs(input)
	assert.Nil(t, err)
	assert.Len(t, r1, 1)
	assert.Equal(t, r1[0], p1)
	input = append(input, "whatever")
	r2, err := pc.getPeerIDs(input)
	assert.NotNil(t, err)
	assert.Len(t, r2, 0)
}
