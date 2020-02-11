package storage

// MockLocalStateManager is a mock use for test purpose
type MockLocalStateManager struct {
}

func (s *MockLocalStateManager) SaveLocalState(state KeygenLocalState) error {
	return nil
}
func (s *MockLocalStateManager) GetLocalState(pubKey string) (KeygenLocalState, error) {
	return KeygenLocalState{}, nil
}
