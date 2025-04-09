package p2p

import (
	"testing"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPickLeader(t *testing.T) {
	t.Run("successful leader selection", func(t *testing.T) {
		peers := []string{
			"16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp",
			"16Uiu2HAm4TmEzUqy3q3Dv7HvdoSboHk5sFj2FH3npiN5vDbJC6gh",
			"16Uiu2HAm2FzqoUdS6Y9Esg2EaGcAG5rVe1r6BFNnmmQr2H3bqafa",
		}

		var peerIDs []peer.ID
		for _, raw := range peers {
			pid, err := peer.Decode(raw)
			require.NoError(t, err)
			peerIDs = append(peerIDs, pid)
		}

		leader, err := PickLeader("HelloWorld", 10, peerIDs)
		require.NoError(t, err)
		assert.Equal(t, peerIDs[1], leader)
	})

	t.Run("single peer returns that peer", func(t *testing.T) {
		singlePeer := peer.ID("16Uiu2HAmACG5DtqmQsHtXg4G2sLS65ttv84e7MrL4kapkjfmhxAp")

		leader, err := PickLeader("HelloWorld", 10, []peer.ID{singlePeer})

		require.NoError(t, err)
		assert.Equal(t, singlePeer, leader)
	})

	t.Run("empty peers list returns error", func(t *testing.T) {
		_, err := PickLeader("HelloWorld", 10, []peer.ID{})
		assert.Error(t, err)
	})

	t.Run("empty msgID returns error", func(t *testing.T) {
		_, err := PickLeader("", 10, []peer.ID{peer.ID("test")})
		assert.Error(t, err)
	})

	t.Run("invalid blockHeight returns error", func(t *testing.T) {
		_, err := PickLeader("HelloWorld", 0, []peer.ID{peer.ID("test")})
		assert.Error(t, err)
	})
}
