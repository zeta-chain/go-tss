package tss

import (
	"testing"

	"gitlab.com/thorchain/tss/go-tss/common"
)

func TestGetPubKeysFromPeerIDs(t *testing.T) {
	common.SetupBech32Prefix()
	input := []string{
		"16Uiu2HAmBdJRswX94UwYj6VLhh4GeUf9X3SjBRgTqFkeEMLmfk2M",
		"16Uiu2HAkyR9dsFqkj1BqKw8ZHAUU2yur6ZLRJxPTiiVYP5uBMeMG",
	}
	result, err := GetPubKeysFromPeerIDs(input)
	if err != nil {
		t.Error(err)
		t.FailNow()
	}
	t.Logf("%+v", result)
}
