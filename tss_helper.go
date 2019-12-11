package go_tss

import (
	"errors"
	"math"

	"github.com/binance-chain/tss-lib/tss"
)

func contains(s []*tss.PartyID, e *tss.PartyID) bool {
	if e == nil {
		return false
	}
	for _, a := range s {
		if *a == *e {
			return true
		}
	}
	return false
}

func getThreshold(value int) (int, error) {
	if value < 0 {
		return 0, errors.New("negative input")
	}
	threshold := int(math.Ceil(float64(value)*2.0/3.0)) - 1
	return threshold, nil
}
