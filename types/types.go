package types

import (
	btss "github.com/binance-chain/tss-lib/tss"
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	Party      btss.Party
	PartyIDMap map[string]*btss.PartyID
}
