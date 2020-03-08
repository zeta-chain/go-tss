package main

import (
	"time"

	go_tss "gitlab.com/thorchain/tss/go-tss"
	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
)

type MockTssServer struct {
}

func (mts *MockTssServer) Start() error {
	return nil
}
func (mts *MockTssServer) Stop() {

}
func (mts *MockTssServer) GetLocalPeerID() string {
	return go_tss.GetRandomPeerID().String()
}
func (mts *MockTssServer) Keygen(req keygen.Request) (keygen.Response, error) {
	return keygen.NewResponse(go_tss.GetRandomPubKey(), "whatever", common.Success, common.NoBlame), nil
}
func (mts *MockTssServer) KeySign(req keysign.Request) (keysign.Response, error) {
	return keysign.NewResponse("", "", common.Success, common.NoBlame), nil
}
func (mts *MockTssServer) GetStatus() common.TssStatus {
	return common.TssStatus{
		Starttime:     time.Now(),
		SucKeyGen:     0,
		FailedKeyGen:  0,
		SucKeySign:    0,
		FailedKeySign: 0,
	}
}
