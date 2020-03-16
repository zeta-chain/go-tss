package common

import (
	"fmt"
	"strings"
)

// NoBlame is empty blame struct
var NoBlame = Blame{}

// Blame is used to store the blame nodes and the fail reason
type Blame struct {
	FailReason string   `json:"fail_reason"`
	BlameNodes []string `json:"blame_peers,omitempty"`
}

// NewBlame create a new instance of Blame
func NewBlame(reason string, blameNodes []string) Blame {
	return Blame{
		FailReason: reason,
		BlameNodes: blameNodes,
	}
}

// IsEmpty check whether it is empty
func (b Blame) IsEmpty() bool {
	return len(b.BlameNodes) == 0 || len(b.FailReason) == 0
}

// String implement fmt.Stringer
func (b Blame) String() string {
	sb := strings.Builder{}
	sb.WriteString("reason:" + b.FailReason + "\n")
	sb.WriteString(fmt.Sprintf("nodes:%+v\n", b.BlameNodes))
	return sb.String()
}

// SetBlame update the field values of Blame
func (b *Blame) SetBlame(reason string, nodes []string) {
	b.FailReason = reason
	b.BlameNodes = append(b.BlameNodes, nodes...)
}

// AddBlameNodes add nodes to the blame list
func (b *Blame) AddBlameNodes(nodePubKeys ...string) {
	for _, node := range nodePubKeys {
		found := false
		for _, item := range b.BlameNodes {
			if item == node {
				found = true
				break
			}
		}
		if !found {
			b.BlameNodes = append(b.BlameNodes, node)
		}
	}
}
