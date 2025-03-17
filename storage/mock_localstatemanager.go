package storage

import (
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
)

// MockLocalStateManager is a mock use for test purpose
type MockLocalStateManager struct {
}

func (s *MockLocalStateManager) SaveLocalState(_ KeygenLocalState) error { return nil }

func (s *MockLocalStateManager) GetLocalState(_ string) (KeygenLocalState, error) {
	return KeygenLocalState{}, nil
}

func (s *MockLocalStateManager) SaveAddressBook(_ map[peer.ID][]maddr.Multiaddr) error {
	return nil
}

func (s *MockLocalStateManager) RetrieveP2PAddresses() ([]maddr.Multiaddr, error) {
	return nil, nil
}
