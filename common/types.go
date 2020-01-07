package common

import (
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

// LocalCacheItem used to cache the unconfirmed broadcast message
type LocalCacheItem struct {
	Msg           *p2p.WireMessage
	Hash          string
	lock          *sync.Mutex
	ConfirmedList map[string]string
}

// KeygenLocalStateItem
type KeygenLocalStateItem struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

type GeneralConfig struct {
	Http       int
	Help       bool
	LogLevel   string
	Pretty     bool
	BaseFolder string
}

type TssConfig struct {
	// KeyGenTimeoutSeconds defines how long do we wait the keygen parties to pass messages along
	KeyGenTimeout time.Duration
	// KeySignTimeoutSeconds defines how long do we wait keysign
	KeySignTimeout time.Duration
	// SyncTimeout defines how long do we wait for sync message
	SyncTimeout time.Duration
	// Pre-parameter define the pre-parameter generations timeout
	PreParamTimeout time.Duration
	// SyncRetry defines how many times we try to sync the peers
	SyncRetry int
}
