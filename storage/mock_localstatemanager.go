package storage

import (
	"github.com/libp2p/go-libp2p/core/peer"

	"gitlab.com/thorchain/tss/go-tss/p2p"
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

func (s *MockLocalStateManager) SaveAddressBook(address map[peer.ID]p2p.AddrList) error {
	return nil
}

func (s *MockLocalStateManager) RetrieveP2PAddresses() (p2p.AddrList, error) {
	return nil, nil
}
