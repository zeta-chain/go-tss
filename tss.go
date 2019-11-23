package go_tss

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"gitlab.com/thorchain/bepswap/thornode/cmd"
)

const (
	Threshold = 2
)

// TssKeyGenInfo the information used by tss key gen
type TssKeyGenInfo struct {
	Party      tss.Party
	PartyIDMap map[string]*tss.PartyID
}

// TSS
type Tss struct {
	comm       *Communication
	logger     zerolog.Logger
	port       int
	server     *http.Server
	wg         sync.WaitGroup
	partyLock  *sync.Mutex
	keyGenInfo *TssKeyGenInfo
	stopChan   chan struct{} // channel to indicate whether we should stop
	queuedMsgs chan TssMessage
}

// NewTss create a new instance of Tss
func NewTss(bootstrapPeers []maddr.Multiaddr, p2pPort, tssPort int) (*Tss, error) {
	if p2pPort == tssPort {
		return nil, errors.New("tss and p2p can't use the same port")
	}
	c, err := NewCommunication(DefaultRendezvous, bootstrapPeers, p2pPort)
	if nil != err {
		return nil, fmt.Errorf("fail to create communication layer: %w", err)
	}
	setupBech32Prefix()
	t := &Tss{
		comm:       c,
		logger:     log.With().Str("module", "tss").Logger(),
		port:       tssPort,
		stopChan:   make(chan struct{}),
		partyLock:  &sync.Mutex{},
		queuedMsgs: make(chan TssMessage, 1024),
	}

	server := &http.Server{
		Addr:    fmt.Sprintf(":%d", tssPort),
		Handler: t.newHandler(true),
	}
	t.server = server
	return t, nil
}

func setupBech32Prefix() {
	config := sdk.GetConfig()
	config.SetBech32PrefixForAccount(cmd.Bech32PrefixAccAddr, cmd.Bech32PrefixAccPub)
	config.SetBech32PrefixForValidator(cmd.Bech32PrefixValAddr, cmd.Bech32PrefixValPub)
	config.SetBech32PrefixForConsensusNode(cmd.Bech32PrefixConsAddr, cmd.Bech32PrefixConsPub)
	config.Seal()
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *Tss) newHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", ping()).Methods(http.MethodGet)
	router.Handle("/keygen", http.HandlerFunc(t.keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keysign)).Methods(http.MethodPost)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *Tss) keygen(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key gen request")
	decoder := json.NewDecoder(r.Body)
	var keygenReq KeyGenReq
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	if t.keyGenInfo != nil {
		resp := KeyGenResp{
			PubKey: "",
			Status: Fail,
		}
		buf, err := json.MarshalIndent(resp, "", "	")
		if nil != err {
			t.logger.Error().Err(err).Msg("fail to marshal response")
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		w.WriteHeader(http.StatusBadRequest)
		_, err = w.Write(buf)
		if nil != err {
			t.logger.Error().Err(err).Msg("fail to write to response")
			return
		}
		return
	}
	if err := t.prepareKeygen(keygenReq); nil != err {

	}
	// start key gen
}
func (t *Tss) prepareKeygen(keygenReq KeyGenReq) error {
	// When using the keygen party it is recommended that you pre-compute the "safe primes" and Paillier secret beforehand because this can take some time.
	// This code will generate those parameters using a concurrency limit equal to the number of available CPU cores.
	preParams, err := keygen.GeneratePreParams(1 * time.Minute)
	if nil != err {
		return fmt.Errorf("fail to generate pre parameters: %w", err)
	}
	priHexBytes, err := base64.StdEncoding.DecodeString(keygenReq.PrivKey)
	if nil != err {
		return fmt.Errorf("fail to decode private key: %w", err)
	}
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if nil != err {
		return fmt.Errorf("fail to hex decode private key: %w", err)
	}
	var keyBytesArray [32]byte
	copy(keyBytesArray[:], rawBytes[:32])
	priKey := secp256k1.PrivKeySecp256k1(keyBytesArray)
	pubKey, err := sdk.Bech32ifyAccPub(priKey.PubKey())
	if nil != err {
		return fmt.Errorf("fail to get account public key: %w", err)
	}
	var localPartyID *tss.PartyID
	var unSortedPartiesID []*tss.PartyID
	for idx, item := range keygenReq.Keys {
		pk, err := sdk.GetAccPubKeyBech32(item)
		if nil != err {
			return err
		}
		key := new(big.Int).SetBytes(pk.Bytes())
		partyID := tss.NewPartyID(strconv.Itoa(idx), "", key)
		if item == pubKey {
			localPartyID = partyID
		}
		unSortedPartiesID = append(unSortedPartiesID, partyID)
	}
	if localPartyID == nil {
		return fmt.Errorf("local party is not in the list")
	}
	partiesID := tss.SortPartyIDs(unSortedPartiesID)

	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	ctx := tss.NewPeerContext(partiesID)
	params := tss.NewParameters(ctx, localPartyID, len(partiesID), Threshold)
	outCh := make(chan tss.Message, len(partiesID))
	endCh := make(chan keygen.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	keyGenParty := keygen.NewLocalParty(params, outCh, endCh, *preParams)

	// You should keep a local mapping of `id` strings to `*PartyID` instances so that an incoming message can have its origin party's `*PartyID` recovered for passing to `UpdateFromBytes` (see below)
	partyIDMap := make(map[string]*tss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}
	t.partyLock.Lock()
	t.keyGenInfo = &TssKeyGenInfo{
		Party:      keyGenParty,
		PartyIDMap: partyIDMap,
	}
	t.partyLock.Unlock()
	wg := &sync.WaitGroup{}
	wg.Add(1)
	// start keygen
	go func() {
		defer wg.Done()
		if err := keyGenParty.Start(); nil != err {
			t.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
			return
		}
	}()
	wg.Add(1)
	go func() {
		defer t.logger.Info().Msg("is it possible it has finished?")
		t.logger.Info().Msg("start to read messages from local party")
		defer wg.Done()
		for {
			select {
			case <-errChan:
				t.logger.Error().Msg("key gen failed")
				return
			case <-t.stopChan:
				return
			case msg := <-outCh:
				t.logger.Info().Msgf(">>>>>>>>>>msg: %s", msg.String())
				buf, r, err := msg.WireBytes()
				if nil != err {
					t.logger.Error().Err(err).Msg("fail to get wire bytes")
				}
				tssMsg := TssMessage{
					Routing: r,
					Message: buf,
				}
				wireBytes, err := json.Marshal(tssMsg)
				if nil != err {
					t.logger.Error().Err(err).Msg("fail to convert tss msg to wire bytes")
					return
				}
				t.logger.Debug().Msgf("broad cast msg to everyone from :%s ", r.From.Id)
				t.comm.Broadcast(nil, wireBytes)
				// drain the in memory queue
				t.drainQueuedMessages()
			case msg := <-endCh:
				t.logger.Info().Msgf("we have done the keygen %s", msg.ECDSAPub.Y().String())
				return
			}
		}
	}()
	t.logger.Info().Msg("Let's wait......")
	wg.Wait()
	return nil
}
func (t *Tss) drainQueuedMessages() {
	if len(t.queuedMsgs) == 0 {
		return
	}
	t.partyLock.Lock()
	keyGenInfo := t.keyGenInfo
	t.partyLock.Unlock()
	for {
		select {
		case m := <-t.queuedMsgs:
			t.logger.Info().Msgf("<<<<<party:%s", m.Routing.From.Id)
			if !t.IsItForCurrentParty(keyGenInfo, m) {
				continue
			}
			partyID, ok := keyGenInfo.PartyIDMap[m.Routing.From.Id]
			if !ok {
				t.logger.Error().Msgf("get message from unknown party :%s", partyID)
				continue
			}
			if _, err := keyGenInfo.Party.UpdateFromBytes(m.Message, partyID, m.Routing.IsBroadcast); nil != err {
				t.logger.Error().Err(err).Msgf("fail to update from bytes,party ID: %s", partyID)
			}
			t.logger.Info().Msgf("update msg from party:%s", m.Routing.From.Id)
		default:
			return
		}
	}
}

type TssMessage struct {
	Routing *tss.MessageRouting `json:"routing"`
	Message []byte              `json:"message"`
}

func (t *Tss) keysign(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.WriteHeader(http.StatusMethodNotAllowed)
		return
	}
	defer func() {
		if err := r.Body.Close(); nil != err {
			t.logger.Error().Err(err).Msg("fail to close request body")
		}
	}()
	t.logger.Info().Msg("receive key sign request")
	decoder := json.NewDecoder(r.Body)
	var keysignReq KeySignReq
	if err := decoder.Decode(&keysignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	// start keysign
}

func ping() http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	})
}
func logMiddleware(verbose bool) mux.MiddlewareFunc {
	return func(handler http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if verbose {
				log.Debug().
					Str("route", r.URL.Path).
					Str("port", r.URL.Port()).
					Str("method", r.Method).
					Msg("HTTP request received")
			}
			handler.ServeHTTP(w, r)
		})
	}
}

// Start Tss server
func (t *Tss) Start(ctx context.Context) error {
	log.Info().Int("port", t.port).Msg("Starting the HTTP server")
	t.wg.Add(1)
	go func() {
		defer t.wg.Done()
		<-ctx.Done()
		close(t.stopChan)
		c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		err := t.server.Shutdown(c)
		if err != nil {
			log.Error().Err(err).Int("port", t.port).Msg("Failed to shutdown the HTTP server gracefully")
		}
	}()

	if err := t.comm.Start(); nil != err {
		return fmt.Errorf("fail to start p2p communication layer: %w", err)
	}

	t.wg.Add(1)
	go t.processComm()
	err := t.server.ListenAndServe()
	t.wg.Wait()
	if err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Int("port", t.port).Msg("Failed to start the HTTP server")
		return err
	}
	log.Info().Int("port", t.port).Msg("The HTTP server has been stopped successfully")
	return nil
}

// processComm is
func (t *Tss) processComm() {
	t.logger.Info().Msg("start to process messages coming from communication channels")
	defer t.wg.Done()
	for {
		select {
		case <-t.stopChan:
			return // time to stop
		case m := <-t.comm.messages:
			t.logger.Info().Msg("<<<<<<<<< inbound")
			var tssMsg TssMessage
			if err := json.Unmarshal(m.Payload, &tssMsg); nil != err {
				t.logger.Error().Err(err).Msgf("fail to unmarshal wire bytes")
				continue
			}
			t.partyLock.Lock()
			keyGenInfo := t.keyGenInfo
			t.partyLock.Unlock()
			if keyGenInfo == nil {
				// we are not doing any keygen at the moment, so we queue it
				t.queuedMsgs <- tssMsg
				continue
			}
			if !t.IsItForCurrentParty(keyGenInfo, tssMsg) {
				continue
			}

			partyID, ok := keyGenInfo.PartyIDMap[tssMsg.Routing.From.Id]
			if !ok {
				t.logger.Error().Msgf("get message from unknown party :%s, peer: %s", partyID, m.PeerID.String())
				continue
			}
			if _, err := keyGenInfo.Party.UpdateFromBytes(tssMsg.Message, partyID, tssMsg.Routing.IsBroadcast); nil != err {
				t.logger.Error().Err(err).Msgf("fail to update from bytes,party ID: %s , peer: %s", partyID, m.PeerID.String())
			}
		}
	}
}

func (t *Tss) IsItForCurrentParty(kgi *TssKeyGenInfo, tssMsg TssMessage) bool {
	if tssMsg.Routing.To == nil {
		t.logger.Info().Msgf("broadcast msg from %s", tssMsg.Routing.From.Id)
		return true
	}
	for _, item := range tssMsg.Routing.To {
		if kgi.Party.PartyID().Id == item.Id {
			t.logger.Info().Msgf("message from %s to %s", tssMsg.Routing.From.Id, item.Id)
			return true
		}
	}
	return false
}
