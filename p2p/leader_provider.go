package p2p

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"sort"
	"strconv"
)

// LeaderNode use the given input buf to calculate a hash , and consistently choose a node as a master coordinate note
func LeaderNode(msgID string, blockHeight int64, pIDs []string) (string, error) {
	if len(pIDs) == 0 || len(msgID) == 0 || blockHeight == 0 {
		return "", errors.New("invalid input for finding the leader")
	}
	keyStore := make(map[string]string)
	hashes := make([]string, len(pIDs))
	for i, el := range pIDs {
		sum := sha256.Sum256([]byte(msgID + strconv.FormatInt(blockHeight, 10) + el))
		encodedSum := hex.EncodeToString(sum[:])
		keyStore[encodedSum] = el
		hashes[i] = encodedSum
	}
	sort.Strings(hashes)
	return keyStore[hashes[0]], nil
}
