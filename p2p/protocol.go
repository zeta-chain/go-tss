package p2p

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/pkg/errors"
)

// ProtocolJoinPartyWithLeader "party with leader" is an entity that is responsible for
// picking a leader for signing a single msg_id and coordinating/propagating
// signature data across the "party".
// The leader is picked based on message data; see LeaderNode().
const ProtocolJoinPartyWithLeader protocol.ID = "/p2p/join-party-leader"

// ProtocolTSS is the protocol for responsible for passing lib-tss messages across peers
// and performing keysign/keygen ceremonies.
const ProtocolTSS protocol.ID = "/p2p/tss"

// ProtocolSignatureNotifier is the protocol for notifying peers for signature data.
// Each peer is responsible for signature verification.
const ProtocolSignatureNotifier protocol.ID = "/p2p/signatureNotifier"

// message types for JoinPartyLeaderComm
const (
	msgTypeRequest  = "request"
	msgTypeResponse = "response"
)

const (
	// PayloadHeaderLen we use first 4 bytes (uint32)
	// to indicate the payload size over the wire.
	PayloadHeaderLen = 4

	// similar to TCP "ack"
	ResponseMessage         = "done"
	ResponseMessageBytesLen = 4
)

// PickLeader picks party leader based on provided input
// The logic is preserved as in the original implementation.
func PickLeader(msgID string, blockHeight int64, peers []peer.ID) (peer.ID, error) {
	if msgID == "" || blockHeight <= 0 || len(peers) == 0 {
		return "", errors.New("invalid input for finding the leader")
	}

	// noop
	if len(peers) == 1 {
		return peers[0], nil
	}

	leader := peers[0]
	leaderHash := hashPeer(msgID, blockHeight, leader)

	for i := 1; i < len(peers); i++ {
		pid := peers[i]
		hash := hashPeer(msgID, blockHeight, pid)

		if hash < leaderHash {
			leaderHash = hash
			leader = pid
		}
	}

	return leader, nil
}

func hashPeer(msgID string, blockHeight int64, pid peer.ID) string {
	concat := fmt.Sprintf("%s%d%s", msgID, blockHeight, pid.String())
	sum := sha256.Sum256([]byte(concat))

	return hex.EncodeToString(sum[:])
}
