package logs

import "github.com/bnb-chain/tss-lib/tss"

const (
	Module    = "module"
	Component = "component"
	MsgID     = "msg_id"
	Peer      = "peer"
	Host      = "host"
	Leader    = "leader"
	Latency   = "latency"
)

func Party(p tss.Party) map[string]any {
	id := p.PartyID()

	return map[string]any{
		"party.id":      id.Id,
		"party.moniker": id.Moniker,
	}
}
