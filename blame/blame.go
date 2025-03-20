package blame

import (
	"bytes"
	"fmt"
	"sync"
)

func NewNode(pk string, blameData, blameSig []byte) Node {
	return Node{
		Pubkey:         pk,
		BlameData:      blameData,
		BlameSignature: blameSig,
	}
}

func (bn *Node) Equal(node Node) bool {
	if bn.Pubkey == node.Pubkey && bytes.Equal(bn.BlameSignature, node.BlameSignature) {
		return true
	}
	return false
}

// NewBlame create a new instance of Blame
func NewBlame(reason string, blameNodes []Node) Blame {
	return Blame{
		FailReason: reason,
		BlameNodes: blameNodes,
		blameLock:  &sync.RWMutex{},
	}
}

func (b Blame) String() string {
	return fmt.Sprintf(
		"{reason=%q, is_unicast=%t, round:%s nodes:%+v}",
		b.FailReason,
		b.IsUnicast,
		b.Round,
		b.BlameNodes,
	)
}

// SetBlame update the field values of Blame
func (b *Blame) SetBlame(reason string, nodes []Node, isUnicast bool, round string) {
	b.blameLock.Lock()
	defer b.blameLock.Unlock()
	b.FailReason = reason
	b.IsUnicast = isUnicast
	b.Round = round
	b.BlameNodes = append(b.BlameNodes, nodes...)
}

func (b *Blame) AlreadyBlame() bool {
	b.blameLock.RLock()
	defer b.blameLock.RUnlock()
	return len(b.BlameNodes) > 0
}

// AddBlameNodes add nodes to the blame list
func (b *Blame) AddBlameNodes(newBlameNodes ...Node) {
	b.blameLock.Lock()
	defer b.blameLock.Unlock()
	for _, node := range newBlameNodes {
		found := false
		for _, el := range b.BlameNodes {
			if node.Equal(el) {
				found = true
				break
			}
		}
		if !found {
			b.BlameNodes = append(b.BlameNodes, node)
		}
	}
}
