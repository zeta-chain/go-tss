package keygen

import (
	bcrypto "github.com/bnb-chain/tss-lib/crypto"

	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/p2p"
)

type TssKeyGen interface {
	GenerateNewKey(keygenReq Request) (*bcrypto.ECPoint, error)
	GetTssKeyGenChannels() chan *p2p.Message
	GetTssCommonStruct() *common.TssCommon
}
