package go_tss

import (
	"context"
	"crypto/elliptic"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/binance-chain/go-sdk/common/types"
	"github.com/binance-chain/tss-lib/crypto"
	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/binance-chain/tss-lib/ecdsa/signing"
	"github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	"github.com/tendermint/btcd/btcec"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"gitlab.com/thorchain/bepswap/thornode/cmd"
)

const (
	Threshold             = 2
	KeyGenTimeoutSeconds  = 120
	KeySignTimeoutSeconds = 30
)

// TssKeyGenInfo the information used by tss key gen
type TssKeyGenInfo struct {
	Party      tss.Party
	PartyIDMap map[string]*tss.PartyID
}

// TssMessage is the message we transfer across the wire to other parties
type TssMessage struct {
	Routing *tss.MessageRouting `json:"routing"`
	Message []byte              `json:"message"`
}

// TSS all the things for TSS
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
	stateLock  *sync.Mutex
	localState KeygenLocalState
	tssLock    *sync.Mutex
	priKey     cryptokey.PrivKey
	preParams  *keygen.LocalPreParams
}

func getPriKey(priKeyString string) (cryptokey.PrivKey, error) {
	priHexBytes, err := base64.StdEncoding.DecodeString(priKeyString)
	if nil != err {
		return nil, fmt.Errorf("fail to decode private key: %w", err)
	}
	rawBytes, err := hex.DecodeString(string(priHexBytes))
	if nil != err {
		return nil, fmt.Errorf("fail to hex decode private key: %w", err)
	}
	var keyBytesArray [32]byte
	copy(keyBytesArray[:], rawBytes[:32])
	priKey := secp256k1.PrivKeySecp256k1(keyBytesArray)
	return priKey, nil
}

// NewTss create a new instance of Tss
func NewTss(bootstrapPeers []maddr.Multiaddr, p2pPort, tssPort int, priKeyBytes []byte) (*Tss, error) {
	if p2pPort == tssPort {
		return nil, errors.New("tss and p2p can't use the same port")
	}
	priKey, err := getPriKey(string(priKeyBytes))
	if err != nil {
		return nil, errors.New("cannot parse the private key")
	}
	c, err := NewCommunication(DefaultRendezvous, bootstrapPeers, p2pPort)
	if nil != err {
		return nil, fmt.Errorf("fail to create communication layer: %w", err)
	}
	setupBech32Prefix()
	localFileName := fmt.Sprintf("localstate-%d.json", tssPort)
	localState, err := GetLocalState(localFileName)
	if nil != err {
		return nil, fmt.Errorf("fail to read local state file: %w", err)
	}
	// When using the keygen party it is recommended that you pre-compute the "safe primes" and Paillier secret beforehand because this can take some time.
	// This code will generate those parameters using a concurrency limit equal to the number of available CPU cores.
	preParams, err := keygen.GeneratePreParams(1 * time.Minute)
	if nil != err {
		return nil, fmt.Errorf("fail to generate pre parameters: %w", err)
	}

	t := &Tss{
		comm:       c,
		logger:     log.With().Str("module", "tss").Logger(),
		port:       tssPort,
		stopChan:   make(chan struct{}),
		partyLock:  &sync.Mutex{},
		queuedMsgs: make(chan TssMessage, 1024),
		stateLock:  &sync.Mutex{},
		localState: localState,
		tssLock:    &sync.Mutex{},
		priKey:     priKey,
		preParams:  preParams,
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
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *Tss) newHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", ping()).Methods(http.MethodGet)
	router.Handle("/keygen", http.HandlerFunc(t.keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keysign)).Methods(http.MethodPost)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pID)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *Tss) getP2pID(w http.ResponseWriter, r *http.Request) {
	localPeerID := t.comm.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
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

	if t.getKeyGenInfo() != nil {
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
	// we only handle one tss request for a given time
	k, err := t.generateNewKey(keygenReq)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to generate new key")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	newPubKey, addr, err := t.getTssPubKey(k)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to bech32 acc pub key")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	if os.Getenv("NET") == "testnet" {
		types.Network = types.TestNetwork
	}
	resp := KeyGenResp{
		PubKey:     newPubKey,
		BNBAddress: addr.String(),
		Status:     Success,
	}
	buf, err := json.Marshal(resp)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	t.logger.Info().Msg(string(buf))
	_, err = w.Write(buf)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
func (t *Tss) getTssPubKey(pubKeyPoint *crypto.ECPoint) (string, types.AccAddress, error) {
	tssPubKey := btcec.PublicKey{
		Curve: btcec.S256(),
		X:     pubKeyPoint.X(),
		Y:     pubKeyPoint.Y(),
	}
	var pubKeyCompressed secp256k1.PubKeySecp256k1
	copy(pubKeyCompressed[:], tssPubKey.SerializeCompressed())
	pubKey, err := sdk.Bech32ifyAccPub(pubKeyCompressed)
	addr := types.AccAddress(pubKeyCompressed.Address().Bytes())
	return pubKey, addr, err
}

func (t *Tss) getParties(keys []string, localPartyKey string) ([]*tss.PartyID, *tss.PartyID, error) {
	var localPartyID *tss.PartyID
	var unSortedPartiesID []*tss.PartyID
	sort.Strings(keys)
	for idx, item := range keys {
		pk, err := sdk.GetAccPubKeyBech32(item)
		if nil != err {
			return nil, nil, fmt.Errorf("fail to get account pub key address(%s): %w", item, err)
		}
		key := new(big.Int).SetBytes(pk.Bytes())
		partyID := tss.NewPartyID(strconv.Itoa(idx), "", key)
		if item == localPartyKey {
			localPartyID = partyID
		}
		unSortedPartiesID = append(unSortedPartiesID, partyID)
	}
	if localPartyID == nil {
		return nil, nil, fmt.Errorf("local party is not in the list")
	}
	partiesID := tss.SortPartyIDs(unSortedPartiesID)
	return partiesID, localPartyID, nil
}

func (t *Tss) generateNewKey(keygenReq KeyGenReq) (*crypto.ECPoint, error) {
	t.tssLock.Lock()
	defer t.tssLock.Unlock()
	pubKey, err := sdk.Bech32ifyAccPub(t.priKey.PubKey())
	if nil != err {
		return nil, fmt.Errorf("fail to genearte the key: %w", err)
	}
	partiesID, localPartyID, err := t.getParties(keygenReq.Keys, pubKey)
	if nil != err {
		return nil, fmt.Errorf("fail to get keygen parties: %w", err)
	}

	keyGenLocalStateItem := KeygenLocalStateItem{
		ParticipantKeys: keygenReq.Keys,
		LocalPartyKey:   pubKey,
	}
	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	ctx := tss.NewPeerContext(partiesID)
	params := tss.NewParameters(ctx, localPartyID, len(partiesID), Threshold)
	outCh := make(chan tss.Message, len(partiesID))
	endCh := make(chan keygen.LocalPartySaveData, len(partiesID))
	errChan := make(chan struct{})
	keyGenParty := keygen.NewLocalParty(params, outCh, endCh, *t.preParams)

	// You should keep a local mapping of `id` strings to `*PartyID` instances so that an incoming message can have its origin party's `*PartyID` recovered for passing to `UpdateFromBytes` (see below)
	partyIDMap := make(map[string]*tss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}

	defer func() {
		t.setKeyGenInfo(nil)
	}()

	// start keygen
	go func() {
		defer t.logger.Info().Msg("keyGenParty finished")
		if err := keyGenParty.Start(); nil != err {
			t.logger.Error().Err(err).Msg("fail to start keygen party")
			close(errChan)
			return
		}
		t.setKeyGenInfo(&TssKeyGenInfo{
			Party:      keyGenParty,
			PartyIDMap: partyIDMap,
		})

	}()

	r, err := t.processKeyGen(errChan, outCh, endCh, keyGenLocalStateItem)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to complete keygen")
		for _, item := range keyGenParty.WaitingFor() {
			t.logger.Error().Err(err).Msgf("we are still waiting for %s", item.Id)
		}
		t.emptyQueuedMessages()
		return nil, err
	}
	return r, nil
}

// emptyQueuedMessages
func (t *Tss) emptyQueuedMessages() {
	for {
		select {
		case m := <-t.queuedMsgs:
			t.logger.Debug().Msgf("drop queued message from %s", m.Routing.From.Id)
		default:
			return
		}
	}
}

func (t *Tss) processKeyGen(errChan chan struct{}, outCh <-chan tss.Message, endCh <-chan keygen.LocalPartySaveData, keyGenLocalStateItem KeygenLocalStateItem) (*crypto.ECPoint, error) {
	defer t.logger.Info().Msg("finished keygen process")
	t.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when keyGenParty return
			t.logger.Error().Msg("key gen failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-t.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(time.Second * KeyGenTimeoutSeconds):
			// we bail out after KeyGenTimeoutSeconds
			return nil, fmt.Errorf("fail to finish keygen with in %d seconds", KeyGenTimeoutSeconds)
		case msg := <-outCh:
			t.logger.Info().Msgf(">>>>>>>>>>msg: %s", msg.String())
			buf, r, err := msg.WireBytes()
			// if we cannot get the wire share, the tss keygen will fail, we just quit.
			if nil != err {
				t.logger.Error().Err(err).Msg("fail to get wire bytes")
				return nil, fmt.Errorf("fail to get wire bytes")
			}
			tssMsg := TssMessage{
				Routing: r,
				Message: buf,
			}
			wireBytes, err := json.Marshal(tssMsg)
			if nil != err {
				return nil, fmt.Errorf("fail to convert tss msg to wire bytes: %w", err)
			}
			t.logger.Info().Msgf("broad cast msg to everyone from :%s ", r.From.Id)
			if err := t.comm.Broadcast(nil, wireBytes); nil != err {
				t.logger.Error().Err(err).Msg("fail to broadcast messages")
			}
			// drain the in memory queue
			t.drainQueuedMessages()
		case msg := <-endCh:
			t.logger.Info().Msgf("we have done the keygen %s", msg.ECDSAPub.Y().String())

			if err := t.addLocalPartySaveData(msg, keyGenLocalStateItem); nil != err {
				return nil, fmt.Errorf("fail to save key gen result to local store: %w", err)
			}
			return msg.ECDSAPub, nil
		}
	}
}

func (t *Tss) addLocalPartySaveData(data keygen.LocalPartySaveData, keyGenLocalStateItem KeygenLocalStateItem) error {
	t.stateLock.Lock()
	defer t.stateLock.Unlock()
	pubKey, addr, err := t.getTssPubKey(data.ECDSAPub)
	if nil != err {
		return fmt.Errorf("fail to get thorchain pubkey: %w", err)
	}
	t.logger.Debug().Msgf("pubkey: %s, bnb address: %s", pubKey, addr)
	keyGenLocalStateItem.PubKey = pubKey
	keyGenLocalStateItem.LocalData = data
	t.localState = append(t.localState, keyGenLocalStateItem)
	localFileName := fmt.Sprintf("localstate-%d.json", t.port)
	return SaveLocalStateToFile(localFileName, t.localState)

}

func (t *Tss) setKeyGenInfo(keyGenInfo *TssKeyGenInfo) {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	t.keyGenInfo = keyGenInfo
}

func (t *Tss) getKeyGenInfo() *TssKeyGenInfo {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	return t.keyGenInfo
}

func (t *Tss) drainQueuedMessages() {
	if len(t.queuedMsgs) == 0 {
		return
	}
	keyGenInfo := t.getKeyGenInfo()
	if nil == keyGenInfo {
		return
	}
	for {
		select {
		case m := <-t.queuedMsgs:
			t.logger.Debug().Msgf("<<<<< queued party:%s", m.Routing.From.Id)
			if !t.isForCurrentParty(keyGenInfo, m) {
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
			t.logger.Debug().Msgf("queued update msg from party:%s", m.Routing.From.Id)
		default:
			return
		}
	}
}

// keysign process keysign request
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
	if t.getKeyGenInfo() != nil {
		t.logger.Error().Msg("another tss key sign is in progress")
		t.writeKeySignResult(w, "", "", Fail)
	}
	signatureData, err := t.signMessage(keysignReq)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to sign message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	t.writeKeySignResult(w, base64.StdEncoding.EncodeToString(signatureData.R), base64.StdEncoding.EncodeToString(signatureData.S), Success)
}

func (t *Tss) writeKeySignResult(w http.ResponseWriter, R, S string, status Status) {
	signResp := KeySignResp{
		R:      R,
		S:      S,
		Status: Success,
	}
	jsonResult, err := json.MarshalIndent(signResp, "", "	")
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json message")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(jsonResult)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write response")
	}
}

// signMessage
func (t *Tss) signMessage(req KeySignReq) (*signing.SignatureData, error) {
	t.tssLock.Lock()
	defer t.tssLock.Unlock()
	keyGenLocalStateItem, err := t.getKeyData(req.PoolPubKey)
	if nil != err {
		return nil, fmt.Errorf("fail to get keygen state data for pubkey(%s): %w", req.PoolPubKey, err)
	}
	msgToSign, err := base64.StdEncoding.DecodeString(req.Message)
	if nil != err {
		return nil, fmt.Errorf("fail to decode message(%s): %w", req.Message, err)
	}
	partiesID, localPartyID, err := t.getParties(keyGenLocalStateItem.ParticipantKeys, keyGenLocalStateItem.LocalPartyKey)
	if nil != err {
		return nil, fmt.Errorf("fail to form key sign party: %w", err)
	}
	// Set up the parameters
	// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
	// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
	// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
	t.logger.Info().Msgf("local party: %s", localPartyID.Id)
	ctx := tss.NewPeerContext(partiesID)
	params := tss.NewParameters(ctx, localPartyID, len(partiesID), Threshold)
	outCh := make(chan tss.Message, len(partiesID))
	endCh := make(chan signing.SignatureData, len(partiesID))
	errCh := make(chan struct{})
	m, err := msgToHashInt(msgToSign)
	if nil != err {
		return nil, fmt.Errorf("fail to convert msg to hash int: %w", err)
	}
	keySignParty := signing.NewLocalParty(m, params, keyGenLocalStateItem.LocalData, outCh, endCh)
	partyIDMap := make(map[string]*tss.PartyID)
	for _, id := range partiesID {
		partyIDMap[id.Id] = id
	}

	defer func() {
		t.setKeyGenInfo(nil)
	}()

	go func() {
		if err := keySignParty.Start(); nil != err {
			t.logger.Error().Err(err).Msg("fail to start key sign party")
			close(errCh)
		}
		t.setKeyGenInfo(&TssKeyGenInfo{
			Party:      keySignParty,
			PartyIDMap: partyIDMap,
		})
	}()

	defer t.emptyQueuedMessages()
	result, err := t.processKeySign(errCh, outCh, endCh)
	if nil != err {
		return nil, fmt.Errorf("fail to process key sign: %w", err)
	}
	t.logger.Info().Msg("successfully sign the message")
	return result, nil
}

func msgToHashInt(msg []byte) (*big.Int, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if nil != err {
		return nil, fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hashToInt(h.Sum(nil), btcec.S256()), nil
}
func hashToInt(hash []byte, c elliptic.Curve) *big.Int {
	orderBits := c.Params().N.BitLen()
	orderBytes := (orderBits + 7) / 8
	if len(hash) > orderBytes {
		hash = hash[:orderBytes]
	}

	ret := new(big.Int).SetBytes(hash)
	excess := len(hash)*8 - orderBits
	if excess > 0 {
		ret.Rsh(ret, uint(excess))
	}
	return ret
}
func (t *Tss) processKeySign(errChan chan struct{}, outCh <-chan tss.Message, endCh <-chan signing.SignatureData) (*signing.SignatureData, error) {
	defer t.logger.Info().Msg("key sign finished")
	t.logger.Info().Msg("start to read messages from local party")
	for {
		select {
		case <-errChan: // when keyGenParty return
			t.logger.Error().Msg("key sign failed")
			return nil, errors.New("error channel closed fail to start local party")
		case <-t.stopChan: // when TSS processor receive signal to quit
			return nil, errors.New("received exit signal")
		case <-time.After(time.Second * KeySignTimeoutSeconds):
			// we bail out after KeyGenTimeoutSeconds
			return nil, fmt.Errorf("fail to sign message with in %d seconds", KeySignTimeoutSeconds)
		case msg := <-outCh:
			t.logger.Info().Msgf(">>>>>>>>>>key sign msg: %s", msg.String())
			buf, r, err := msg.WireBytes()
			if nil != err {
				t.logger.Error().Err(err).Msg("fail to get wire bytes")
				continue
			}
			tssMsg := TssMessage{
				Routing: r,
				Message: buf,
			}
			wireBytes, err := json.Marshal(tssMsg)
			if nil != err {
				return nil, fmt.Errorf("fail to convert tss msg to wire bytes: %w", err)
			}
			t.logger.Info().Msgf("broad cast msg to everyone from :%s ", r.From.Id)
			if err := t.comm.Broadcast(nil, wireBytes); nil != err {
				t.logger.Error().Err(err).Msg("fail to broadcast messages")
			}
			// drain the in memory queue
			t.drainQueuedMessages()
		case msg := <-endCh:
			t.logger.Info().Msg("we have done the key sign")
			return &msg, nil
		}
	}
}

func (t *Tss) getKeyData(pubKey string) (KeygenLocalStateItem, error) {
	var emptyKeyGenLocalStateItem KeygenLocalStateItem
	t.stateLock.Lock()
	defer t.stateLock.Unlock()
	for _, item := range t.localState {
		if item.PubKey == pubKey {
			return item, nil
		}
	}
	return emptyKeyGenLocalStateItem, fmt.Errorf("didnot find keygen state for(%s)", pubKey)
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
func (t *Tss) Start(ctx context.Context, priKeyBytes []byte) error {
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

	if err := t.comm.Start(priKeyBytes); nil != err {
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

			keyGenInfo := t.getKeyGenInfo()
			if keyGenInfo == nil {
				// we are not doing any keygen at the moment, so we queue it
				t.logger.Info().Msg("queue the message")
				t.queuedMsgs <- tssMsg
				continue
			}
			if !t.isForCurrentParty(keyGenInfo, tssMsg) {
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

func (t *Tss) isForCurrentParty(kgi *TssKeyGenInfo, tssMsg TssMessage) bool {
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
