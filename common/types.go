package common

import (
	"errors"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"

	"gitlab.com/thorchain/tss/go-tss/p2p"
)

const (
	BlameHashCheck     = "hash check failed"
	BlameNodeSyncCheck = "node sync failed"
	BlameTssTimeout    = "Tss timeout"
)

var (
	ByPassGeneratePreParam = false
	ErrHashFromOwner       = errors.New("hashcheck error from data owner")
	ErrHashFromPeer        = errors.New("hashcheck error from peer")
	ErrTssTimeOut          = errors.New("error Tss Timeout")
	ErrNodeSync            = errors.New("error nodesync Timeout")
)

// LocalCacheItem used to cache the unconfirmed broadcast message
type LocalCacheItem struct {
	Msg           *p2p.WireMessage
	Hash          string
	lock          *sync.Mutex
	ConfirmedList map[string]string
}

// Blame is used to store the blame nodes and the fail reason
type Blame struct {
	FailReason string   `json:"fail_reason"`
	BlameNodes []string `json:"blame_peers"`
}

// KeygenLocalStateItem
type KeygenLocalStateItem struct {
	PubKey          string                    `json:"pub_key"`
	LocalData       keygen.LocalPartySaveData `json:"local_data"`
	ParticipantKeys []string                  `json:"participant_keys"` // the paticipant of last key gen
	LocalPartyKey   string                    `json:"local_party_key"`
}

type GeneralConfig struct {
	TssAddr    string
	InfoAddr   string
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

type TssStatus struct {
	// Starttime indicates when the Tss server starts
	Starttime time.Time `json:"start_time"`
	// SucKeyGen indicates how many times we run keygen successfully
	SucKeyGen uint64 `json:"successful_keygen"`
	// FailedKeyGen indicates how many times we run keygen unsuccessfully(the invalid http request is not counted as
	// the failure of keygen)
	FailedKeyGen uint64 `json:"failed_keygen"`
	// SucKeySign indicates how many times we run keySign successfully
	SucKeySign uint64 `json:"successful_keysign"`
	// FailedKeySign indicates how many times we run keysign unsuccessfully(the invalid http request is not counted as
	// the failure of keysign)
	FailedKeySign uint64 `json:"failed_keysign"`
	// CurrKeygen indicates the which keygen round we are in
	CurrKeyGen string `json:"current_keygen"`
	// CurrKeySign indicates the which keysign round we are in
	CurrKeySign string `json:"current_keysign"`
}
