package p2p

import (
	"errors"
	"sync"

	"github.com/libp2p/go-libp2p-core/peer"
)

type PeerStatus struct {
	peersResponse  map[peer.ID]bool
	peerStatusLock *sync.RWMutex
	newFound       chan bool
}

func NewPeerStatus(peerNodes []peer.ID, myPeerID peer.ID) *PeerStatus {
	dat := make(map[peer.ID]bool)
	for _, el := range peerNodes {
		if el == myPeerID {
			continue
		}
		dat[el] = false
	}
	peerStatus := &PeerStatus{
		peersResponse:  dat,
		peerStatusLock: &sync.RWMutex{},
		newFound:       make(chan bool, len(peerNodes)),
	}
	return peerStatus
}

func (ps *PeerStatus) getCoordinationStatus() bool {
	_, offline := ps.getPeersStatus()
	if len(offline) == 0 {
		return true
	}
	return false
}

func (ps *PeerStatus) getPeersStatus() ([]peer.ID, []peer.ID) {
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

func (ps *PeerStatus) updatePeer(peerNode peer.ID) (bool, error) {
	ps.peerStatusLock.Lock()
	defer ps.peerStatusLock.Unlock()
	val, ok := ps.peersResponse[peerNode]
	if !ok {
		return false, errors.New("key not found")
	}
	if !val {
		ps.peersResponse[peerNode] = true
		return true, nil
	}
	return false, nil
}
