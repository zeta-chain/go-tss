package p2p

import (
	"errors"
	"sync"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/rs/zerolog/log"

	"github.com/zeta-chain/go-tss/messages"
)

// NoLeader will be dropped.
const NoLeader = "NONE"

type peerStatus struct {
	peersResponse  map[peer.ID]bool
	peerStatusLock *sync.RWMutex
	allPeers       []peer.ID
	notify         chan bool
	leaderResponse *messages.JoinPartyLeaderComm
	leader         peer.ID
	threshold      int
	reqCount       int
}

func (ps *peerStatus) getLeaderResponse() *messages.JoinPartyLeaderComm {
	ps.peerStatusLock.RLock()
	defer ps.peerStatusLock.RUnlock()
	return ps.leaderResponse
}

func (ps *peerStatus) setLeaderResponse(resp *messages.JoinPartyLeaderComm) {
	ps.peerStatusLock.Lock()
	defer ps.peerStatusLock.Unlock()
	ps.leaderResponse = resp
}

func (ps *peerStatus) getLeader() peer.ID {
	ps.peerStatusLock.RLock()
	defer ps.peerStatusLock.RUnlock()
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
	peerStatus := &peerStatus{
		peersResponse:  dat,
		peerStatusLock: &sync.RWMutex{},
		notify:         make(chan bool, len(peerNodes)),
		allPeers:       peerNodes,
		leader:         leaderID,
		threshold:      threshold,
		reqCount:       0,
	}
	return peerStatus
}

func (ps *peerStatus) getCoordinationStatus() bool {
	_, offline := ps.getPeersStatus()
	return len(offline) == 0
}

func (ps *peerStatus) getAllPeers() []peer.ID {
	ps.peerStatusLock.RLock()
	defer ps.peerStatusLock.RUnlock()
	return ps.allPeers
}

func (ps *peerStatus) getPeersStatus() ([]peer.ID, []peer.ID) {
	var online []peer.ID
	var offline []peer.ID
	ps.peerStatusLock.RLock()
	defer ps.peerStatusLock.RUnlock()
	for peerNode, val := range ps.peersResponse {
		if val {
			online = append(online, peerNode)
		} else {
			offline = append(offline, peerNode)
		}
	}

	return online, offline
}

func (ps *peerStatus) updatePeer(peerNode peer.ID) (bool, error) {
	ps.peerStatusLock.Lock()
	defer ps.peerStatusLock.Unlock()
	val, ok := ps.peersResponse[peerNode]
	if !ok {
		return false, errors.New("key not found")
	}

	if ps.leader == NoLeader {
		if !val {
			ps.peersResponse[peerNode] = true
			return true, nil
		}
		return false, nil
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
