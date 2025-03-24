package p2p

import "github.com/libp2p/go-libp2p/core/protocol"

// ProtocolJoinPartyWithLeader "party with leader" is an entity that is responsible for
// picking a leader for signing a single msg_id and coordinating/propagating
// signature data across the "party".
// The leader is picked based on message data; see LeaderNode().
const ProtocolJoinPartyWithLeader protocol.ID = "/p2p/join-party-leader"

// ProtocolTSS todo
const ProtocolTSS protocol.ID = "/p2p/tss"

// ProtocolSignatureNotifier todo
const ProtocolSignatureNotifier protocol.ID = "/p2p/signatureNotifier"
