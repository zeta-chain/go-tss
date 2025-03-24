package p2p

import (
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/rs/zerolog/log"

	"github.com/zeta-chain/go-tss/messages"
)

type peerStatus struct {
	peersResponse  map[peer.ID]bool
	allPeers       []peer.ID
	notify         chan bool
	leaderResponse *messages.JoinPartyLeaderComm
	leader         peer.ID
	threshold      int
	reqCount       int

	mu sync.RWMutex
}

func (ps *peerStatus) getLeaderResponse() *messages.JoinPartyLeaderComm {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return ps.leaderResponse
}

func (ps *peerStatus) setLeaderResponse(resp *messages.JoinPartyLeaderComm) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	ps.leaderResponse = resp
}

func (ps *peerStatus) getLeader() peer.ID {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return ps.leader
}

func newPeerStatus(peerNodes []peer.ID, myPeerID, leaderID peer.ID, threshold int) *peerStatus {
	dat := make(map[peer.ID]bool)
	for _, el := range peerNodes {
		if el == myPeerID {
			continue
		}

		dat[el] = false
	}

	return &peerStatus{
		peersResponse: dat,
		notify:        make(chan bool, len(peerNodes)),
		allPeers:      peerNodes,
		leader:        leaderID,
		threshold:     threshold,
		reqCount:      0,
		mu:            sync.RWMutex{},
	}
}

//nolint:unused // used in tests
func (ps *peerStatus) getCoordinationStatus() bool {
	_, offline := ps.getPeersStatus()
	return len(offline) == 0
}

func (ps *peerStatus) getAllPeers() []peer.ID {
	ps.mu.RLock()
	defer ps.mu.RUnlock()

	return ps.allPeers
}

func (ps *peerStatus) getPeersStatus() ([]peer.ID, []peer.ID) {
	var offline, online []peer.ID

	ps.mu.RLock()
	defer ps.mu.RUnlock()

	for peerID, isOnline := range ps.peersResponse {
		if isOnline {
			online = append(online, peerID)
			continue
		}

		offline = append(offline, peerID)
	}

	return online, offline
}

func (ps *peerStatus) updatePeer(peerNode peer.ID) (bool, error) {
	ps.mu.Lock()
	defer ps.mu.Unlock()

	val, ok := ps.peersResponse[peerNode]
	if !ok {
		return false, errors.New("key not found")
	}

	// we already have enough participants
	if ps.reqCount >= ps.threshold {
		return false, nil
	}

	if !val {
		ps.peersResponse[peerNode] = true
		ps.reqCount++
		log.Debug().Msgf("leader has %d out of %d participants", ps.reqCount, ps.threshold)
		if ps.reqCount >= ps.threshold {
			return true, nil
		}
	}
	return false, nil
}
