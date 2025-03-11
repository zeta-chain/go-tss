package main

import (
	"errors"

	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/zeta-chain/go-tss/blame"
	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/keysign"
)

type MockTssServer struct {
	failToStart   bool
	failToKeyGen  bool
	failToKeySign bool
}

func (mts *MockTssServer) Start() error {
	if mts.failToStart {
		return errors.New("you ask for it")
	}
	return nil
}

func (mts *MockTssServer) Stop() {
}

func (mts *MockTssServer) GetLocalPeerID() string {
	return conversion.GetRandomPeerID().String()
}

func (mts *MockTssServer) GetKnownPeers() []peer.AddrInfo {
	return []peer.AddrInfo{}
}

func (mts *MockTssServer) Keygen(req keygen.Request) (keygen.Response, error) {
	if mts.failToKeyGen {
		return keygen.Response{}, errors.New("you ask for it")
	}
	return keygen.NewResponse(
		common.ECDSA,
		conversion.GetRandomPubKey(),
		"whatever",
		common.Success,
		blame.Blame{},
	), nil
}

func (mts *MockTssServer) KeygenAllAlgo(req keygen.Request) ([]keygen.Response, error) {
	if mts.failToKeyGen {
		return []keygen.Response{{}}, errors.New("you ask for it")
	}
	return []keygen.Response{
		keygen.NewResponse(common.ECDSA, conversion.GetRandomPubKey(), "whatever", common.Success, blame.Blame{}),
		keygen.NewResponse(common.EdDSA, conversion.GetRandomPubKey(), "whatever", common.Success, blame.Blame{}),
	}, nil
}

func (mts *MockTssServer) KeySign(req keysign.Request) (keysign.Response, error) {
	if mts.failToKeySign {
		return keysign.Response{}, errors.New("you ask for it")
	}
	newSig := keysign.NewSignature("", "", "", "")
	return keysign.NewResponse([]keysign.Signature{newSig}, common.Success, blame.Blame{}), nil
}
