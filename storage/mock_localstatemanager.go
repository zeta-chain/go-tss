package storage

import (
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-peerstore/addr"
)

// MockLocalStateManager is a mock use for test purpose
type MockLocalStateManager struct {
}

func (s *MockLocalStateManager) SaveLocalState(state KeygenLocalState) error {
	return nil
}

func (s *MockLocalStateManager) GetLocalState(pubKey string) (KeygenLocalState, error) {
	return KeygenLocalState{}, nil
}

func (s *MockLocalStateManager) SaveAddressBook(address map[peer.ID]addr.AddrList) error {
	return nil
}

func (s *MockLocalStateManager) RetrieveP2PAddresses() (addr.AddrList, error) {
	return nil, nil
}
