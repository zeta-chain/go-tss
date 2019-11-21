package main

import (
	"crypto/rand"
	"math/big"
	"strconv"
	"time"

	"github.com/binance-chain/tss-lib/common"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/binance-chain/tss-lib/tss"

	"github.com/decred/dcrd/dcrec/secp256k1"
	"github.com/ipfs/go-log"
)

const PARTIESNUM = 4
const THRESHOLD = 2

var Logger = log.Logger("thorchain-tssmain")

type localkey struct {
	sk []byte
	pk []byte
}

func secpkeygen() localkey {

	skbyte, x, y, err := secp256k1.GenerateKey(rand.Reader)
	if err != nil {
		panic(err)
	}
	pubkey := secp256k1.NewPublicKey(x, y)

	localkey := localkey{
		sk: skbyte,
		pk: pubkey.SerializeCompressed()}

	return localkey

}

func main() {
	log.SetDebugLogging()

	Logger.Infof(">>>>>>>>>>>>>>>>>>>>>>we start the TSS\n")

	PreParamsarray := []*keygen.LocalPreParams{}

	for i := 0; i < PARTIESNUM; i++ {

		preParams, _ := keygen.GeneratePreParams(1 * time.Minute)
		PreParamsarray = append(PreParamsarray, preParams)
	}
	// node, err := net.NewNode("Tssconfig.json")
	// if err != nil {
	//	return
	// }
	// node.StartNode()

	// pubkeys := []localkey{}
	unSortedPartiesID := []*tss.PartyID{}
	for i := 0; i < PARTIESNUM; i++ {
		keypair := secpkeygen()
		// pubkeys = append(pubkeys, keypair)
		compressedgitint := new(big.Int).SetBytes(keypair.pk)
		nodeid := strconv.FormatInt(int64(i), 10)
		unSortedPartiesID = append(unSortedPartiesID, tss.NewPartyID(nodeid, "", compressedgitint))
	}

	partiesID := tss.SortPartyIDs(unSortedPartiesID)

	ctx := tss.NewPeerContext(partiesID)

	parties := make([]*keygen.LocalParty, 0, len(unSortedPartiesID))

	partyIDMap := make(map[string]*tss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}

	errCh := make(chan *tss.Error, len(partiesID))
	outCh := make(chan tss.Message, len(partiesID))
	endCh := make(chan keygen.LocalPartySaveData, len(partiesID))
	//
	for i, eachparty := range partiesID {
		params := tss.NewParameters(ctx, eachparty, len(partiesID), THRESHOLD)
		P := keygen.NewLocalParty(params, outCh, endCh, *PreParamsarray[i]).(*keygen.LocalParty) // Omit the last arg to compute the pre-params in round 1
		parties = append(parties, P)
		go func(P *keygen.LocalParty) {
			err := P.Start()
			if err != nil {
				errCh <- err
			}

		}(P)
		break
	}
keygen:
	for {
		select {
		case err := <-errCh:
			common.Logger.Errorf("Error: %s", err)
			break keygen

		case msg := <-outCh:
			dest := msg.GetTo()
			if dest == nil {
				Logger.Infof("get from %s\n", msg.GetFrom().Index)
				Logger.Infof("get from %s\n", msg.Type())

				for _, p := range parties {
					if p.PartyID().Index == msg.GetFrom().Index {
						continue
					}
					bz, _, err := msg.WireBytes()
					if err != nil {
						errCh <- p.WrapError(err)
						return
					}
					go func(party tss.Party) {
						_, err := party.UpdateFromBytes(bz, msg.GetFrom(), msg.IsBroadcast())
						if nil != err {
							Logger.Error(err)
						}
					}(p)
				}
			} else {
				for _, eachdst := range dest {
					if eachdst.Index == msg.GetFrom().Index {
						Logger.Error("party %d tried to send a message to itself (%d)", dest[0].Index, msg.GetFrom().Index)
					}

					p := parties[eachdst.Index]
					bz, _, err := msg.WireBytes()
					if err != nil {
						errCh <- p.WrapError(err)
						return
					}
					go func(party tss.Party) {
						_, err := p.UpdateFromBytes(bz, msg.GetFrom(), msg.IsBroadcast())
						if nil != err {
							Logger.Error(err)
						}
					}(p)

				}

			}

		case msg := <-endCh:
			Logger.Infof("we have done the keygen %s", msg.ECDSAPub.Y().String())
			break keygen
		}
	}

}
