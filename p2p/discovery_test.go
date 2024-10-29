package p2p

import (
	"crypto/rand"
	"encoding/base64"
	"testing"
	"time"

	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/stretchr/testify/assert"
)

func TestDiscovery(t *testing.T) {
	GossipInterval = 1 * time.Second
	bootstrapPeer := "/ip4/127.0.0.1/tcp/2220/p2p/16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh"
	bootstrapPeerID, err := peer.Decode("16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh")

	bootstrapPrivKey := "6LABmWB4iXqkqOJ9H0YFEA2CSSx6bA7XAKGyI/TDtas="
	externalIP := "127.0.0.1"
	var whitelistedPeers []peer.ID

	//fakeExternalMultiAddr := "/ip4/127.0.0.1/tcp/2220"
	validMultiAddr, err := maddr.NewMultiaddr(bootstrapPeer)
	assert.NoError(t, err)
	privKey, err := base64.StdEncoding.DecodeString(bootstrapPrivKey)
	assert.NoError(t, err)

	sk1, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	sk1raw, _ := sk1.Raw()
	assert.NoError(t, err)

	sk2, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	assert.NoError(t, err)
	sk2raw, _ := sk2.Raw()
	sk3, _, err := crypto.GenerateSecp256k1Key(rand.Reader)
	assert.NoError(t, err)
	sk3raw, _ := sk3.Raw()
	id1, err := peer.IDFromPrivateKey(sk1)
	id2, err := peer.IDFromPrivateKey(sk2)
	id3, err := peer.IDFromPrivateKey(sk3)
	whitelistedPeers = append(whitelistedPeers, id1, id2, id3, bootstrapPeerID)

	comm, err := NewCommunication(nil, 2220, externalIP, whitelistedPeers)
	assert.NoError(t, err)
	assert.NoError(t, comm.Start(privKey))
	defer comm.Stop()

	comm2, err := NewCommunication([]maddr.Multiaddr{validMultiAddr}, 2221, externalIP, whitelistedPeers)
	assert.NoError(t, err)
	err = comm2.Start(sk1raw)
	assert.NoError(t, err)
	defer comm2.Stop()

	// we connect to an invalid peer and see

	//id, err := peer.IDFromPrivateKey(sk2)
	//assert.NoError(t, err)
	//invalidAddr := "/ip4/127.0.0.1/tcp/2220/p2p/" + id.String()
	//invalidMultiAddr, err := maddr.NewMultiaddr(invalidAddr)
	assert.NoError(t, err)
	comm3, err := NewCommunication([]maddr.Multiaddr{validMultiAddr}, 2222, externalIP, whitelistedPeers)
	assert.NoError(t, err)
	err = comm3.Start(sk2raw)
	defer comm3.Stop()

	// we connect to one invalid and one valid address

	comm4, err := NewCommunication([]maddr.Multiaddr{validMultiAddr}, 2223, externalIP, whitelistedPeers)
	assert.NoError(t, err)
	err = comm4.Start(sk3raw)
	assert.NoError(t, err)
	defer comm4.Stop()

	time.Sleep(5 * time.Second)

	assert.Equal(t, len(comm.host.Peerstore().Peers()), 4)
	assert.Equal(t, len(comm2.host.Peerstore().Peers()), 4)
	assert.Equal(t, len(comm3.host.Peerstore().Peers()), 4)
	assert.Equal(t, len(comm4.host.Peerstore().Peers()), 4)
}
