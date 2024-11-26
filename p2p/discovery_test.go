package p2p

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDiscovery(t *testing.T) {
	// ARRANGE
	// Given test logger
	logger := zerolog.New(zerolog.NewTestWriter(t))

	// Given Communication options
	opts := []CommOpt{
		// fast test cases
		WithGossipInterval(500 * time.Millisecond), WithLogger(logger),
	}

	deferStop := func(c *Communication) {
		t.Cleanup(func() { require.NoError(t, c.Stop()) })
	}

	// Given expected number of peers
	const expectedPeersCount = 4

	// Given bootstrap peer
	bootstrapPrivKey := "6LABmWB4iXqkqOJ9H0YFEA2CSSx6bA7XAKGyI/TDtas="
	bootstrapPeer := "/ip4/127.0.0.1/tcp/2220/p2p/16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh"
	bootstrapPeerID, err := peer.Decode("16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh")
	require.NoError(t, err)

	bpMultiAddr, err := maddr.NewMultiaddr(bootstrapPeer)
	require.NoError(t, err)

	bpPeerPrivKey, err := base64.StdEncoding.DecodeString(bootstrapPrivKey)
	require.NoError(t, err)

	// Given invalid second bootstrap peer

	// Given 3 other peers
	peer2 := newTestPeer(t)
	peer3 := newTestPeer(t)
	peer4 := newTestPeer(t)

	// Given whitelisted peers
	whitelistedPeers := []peer.ID{bootstrapPeerID, peer2.id, peer3.id, peer4.id}

	// Given invalid bootstrap peer
	invalidPeerRaw := "/ip4/127.0.0.1/tcp/3333/p2p/16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gf"
	invalidPeerAddr, err := maddr.NewMultiaddr(invalidPeerRaw)
	require.NoError(t, err)

	const externalIP = "127.0.0.1"

	// ACT
	// Start peer1 (bootstrap peer)
	comm1, err := NewCommunication(nil, 2220, externalIP, whitelistedPeers, opts...)
	assert.NoError(t, err)
	assert.NoError(t, comm1.Start(bpPeerPrivKey))
	deferStop(comm1)

	// Start peer2 (comm2)
	comm2, err := NewCommunication([]maddr.Multiaddr{bpMultiAddr}, 2221, externalIP, whitelistedPeers, opts...)
	assert.NoError(t, err)
	assert.NoError(t, comm2.Start(peer2.skBytes))
	deferStop(comm2)

	// Start peer3 (comm3)
	comm3, err := NewCommunication([]maddr.Multiaddr{bpMultiAddr}, 2222, externalIP, whitelistedPeers, opts...)
	assert.NoError(t, err)
	assert.NoError(t, comm3.Start(peer3.skBytes))
	deferStop(comm3)

	// Start peer4 (comm4).
	comm4, err := NewCommunication([]maddr.Multiaddr{bpMultiAddr, invalidPeerAddr}, 2223, externalIP, whitelistedPeers, opts...)
	assert.NoError(t, err)
	assert.NoError(t, comm4.Start(peer4.skBytes))
	deferStop(comm4)

	// ASSERT
	// Wait until all Communication{} have the same number of peers (expectedPeersCount - self)
	eventually := func(c *Communication, name string) {
		check := func(ct *assert.CollectT) {
			assert.Equal(ct, expectedPeersCount-1, len(c.host.Network().Peers()), name)
		}

		assert.EventuallyWithT(t, check, 10*time.Second, 500*time.Millisecond, name)
	}

	eventually(comm1, "comm1")
	eventually(comm2, "comm2")
	eventually(comm3, "comm3")
	eventually(comm4, "comm4")

	// Extra check to make sure that `comm1` has
	assert.Equal(t, expectedPeersCount-1, len(comm1.discovery.getKnownPeers()))

	for _, ai := range comm1.discovery.getKnownPeers() {
		assert.LessOrEqual(t, len(ai.Addrs), 4, "%s has more than 4 addresses (%d)?", ai.ID.String(), len(ai.Addrs))
	}
}

type testPeer struct {
	sk      crypto.PrivKey
	skBytes []byte
	id      peer.ID
}

func newTestPeer(t *testing.T) testPeer {
	t.Helper()

	sk, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	require.NoError(t, err)

	skBytes, err := sk.Raw()
	require.NoError(t, err)

	peerID, err := peer.IDFromPrivateKey(sk)
	require.NoError(t, err)

	return testPeer{
		sk:      sk,
		skBytes: skBytes,
		id:      peerID,
	}
}
