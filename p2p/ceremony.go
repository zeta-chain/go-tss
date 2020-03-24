package p2p

import (
	"sort"
	"time"

	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/rs/zerolog/log"
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
	timeoutTimer      *time.Timer
	TimeoutChan       chan []string //
}

// NewCeremony create a new instance of ceremony
func NewCeremony(messageID string, threshold uint32, peers []peer.ID, timeout time.Duration) *Ceremony {
	c := &Ceremony{
		ID:                messageID,
		Threshold:         threshold,
		JoinPartyRequests: []*JoinParty{},
		Status:            GatheringParties,
		Peers:             peers,
		TimeoutChan:       make(chan []string, threshold),
	}
	c.timeoutTimer = time.AfterFunc(timeout, c.timeoutCallback)
	return c
}

// IsReady do we have enough players to start the game?
func (c *Ceremony) IsReady() bool {
	// we got enough nodes
	if len(c.JoinPartyRequests) >= int(c.Threshold) {
		// acquire enough party , thus cancel the timeout timer
		defer c.timeoutTimer.Stop()
		return true
	}
	return false
}

func (c *Ceremony) timeoutCallback() {
	peers := c.GetParties()
	log.Info().Str("module", "ceremony").Msgf("timeout: online peers:%+v", peers)
	for i := 0; i < int(c.Threshold); i++ {
		c.TimeoutChan <- peers
	}
	close(c.TimeoutChan)
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

// IsPartyExist double check whether the given party already in the list
func (c *Ceremony) IsPartyExist(id peer.ID) bool {
	for _, item := range c.JoinPartyRequests {
		if item.Peer == id {
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
