package storage

import (
	"testing"

	"gitlab.com/thorchain/tss/go-tss/conversion"
	. "gopkg.in/check.v1"
)

func TestPackageBC(t *testing.T) { TestingT(t) }

type KeySignTestSuite struct {
	fsm *FileStateMgr
}

var _ = Suite(&KeySignTestSuite{})

func (s *KeySignTestSuite) SetUpSuite(c *C) {
	conversion.SetupBech32Prefix()

	baseDir := "../test_data/keysign_data/ecdsa-old" // Adjust this path as needed
	var err error
	s.fsm, err = NewFileStateMgr(baseDir, "")
	c.Assert(err, IsNil)
}

func (s *KeySignTestSuite) TestLoadOldFormatLocalState(c *C) {
	pubKey := "thorpub1addwnpepqv6xp3fmm47dfuzglywqvpv8fdjv55zxte4a26tslcezns5czv586u2fw33" // This assumes that the public key "0" corresponds to the file "0.json"

	localState, err := s.fsm.GetLocalState(pubKey)
	// c.Errorf("localState.LocalData %w", localState.LocalData)
	c.Assert(err, IsNil)

	// Perform assertions to check the contents of localState
	// For example, you might want to check specific fields to ensure they match expected values
	c.Assert(localState.PubKey, Equals, pubKey)
	c.Assert(localState.LocalData, NotNil)
}
