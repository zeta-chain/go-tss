package p2p

import (
	"hash/fnv"
)

// LeaderNode use the given input buf to calculate a hash , and consistently choose a node as a master coordinate note
func LeaderNode(buf []byte, numNodes int32) (int32, error) {
	h := fnv.New32()
	if _, err := h.Write(buf); err != nil {
		return -1, err
	}
	result := int32(h.Sum32())
	if result < 0 {
		result = result * -1
	}
	return result % numNodes, nil
}
