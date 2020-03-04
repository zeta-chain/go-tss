package p2p

import (
	"sort"

	"github.com/libp2p/go-libp2p-core/peer"
)

type CeremonyStatus int

const (
	NA CeremonyStatus = iota
	GatheringParties
	Failed
	Finished
)

// String implement fmt.Stringer
func (cs CeremonyStatus) String() string {
	switch cs {
	case NA:
		return "unknown"
	case GatheringParties:
		return "gathering"
	case Failed:
		return "failed"
	case Finished:
		return "finished"
	}
	return ""
}

// Ceremony can be a keygen ceremony or key sign ceremony
type Ceremony struct {
	ID                string         // ID , it should be the hash of keygen / keysign payload
	Threshold         uint32         // how many peers are required to finish the ceremony
	JoinPartyRequests []*JoinParty   // all the join party requests
	Status            CeremonyStatus // ceremony status
	Peers             []peer.ID      // peers
}

// IsReady do we have enough players to start the game?
func (c *Ceremony) IsReady() bool {
	// we got enough nodes
	if len(c.JoinPartyRequests) >= int(c.Threshold) {
		return true
	}
	return false
}

// ValidPeer is going to validate the peer against the peers list in ceremony
// Ceremony will be create by the leader node,if the peer is unknown to the ceremony , it will reject the join party request.
func (c *Ceremony) ValidPeer(id peer.ID) bool {
	for _, item := range c.Peers {
		if item == id {
			return true
		}
	}
	return false
}

// GetParties return a list of peer id that will be doing the upcoming ceremony
func (c *Ceremony) GetParties() []string {
	var parties []string
	for _, item := range c.JoinPartyRequests {
		parties = append(parties, item.Peer.String())
	}
	sort.SliceStable(parties, func(i, j int) bool {
		return parties[i] < parties[j]
	})
	return parties
}
