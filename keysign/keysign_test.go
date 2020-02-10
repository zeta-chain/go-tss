package keysign

import (
	"testing"

	. "gopkg.in/check.v1"

	"gitlab.com/thorchain/tss/go-tss/common"
)

func TestPackage(t *testing.T) { TestingT(t) }

type TssTestSuite struct{}

var _ = Suite(&TssTestSuite{})

func (t *TssTestSuite) SetUpSuite(c *C) {
	common.InitLog("info", true, "keysign_test")
}
