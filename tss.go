package tss

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"net/http"
	"path/filepath"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/binance-chain/tss-lib/ecdsa/keygen"
	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	"github.com/libp2p/go-libp2p-core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"gitlab.com/thorchain/thornode/cmd"
)

const (
	// KeyGenTimeoutSeconds how long do we wait the keygen parties to pass messages along
	KeyGenTimeoutSeconds = 120
	// KeySignTimeoutSeconds how long do we wait keysign
	KeySignTimeoutSeconds = 30
)

var (
	byPassGeneratePreParam = false
	HashFromOwnerErr       = fmt.Errorf("msg from data owner")
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	Party      btss.Party
	PartyIDMap map[string]*btss.PartyID
}

type QueuedMsg struct {
	wrappedMsg *WrappedMessage
	peerID     string
}

// Tss all the things for TSS
type Tss struct {
	comm                *Communication
	logger              zerolog.Logger
	port                int
	server              *http.Server
	wg                  sync.WaitGroup
	partyLock           *sync.Mutex
	partyInfo           *PartyInfo
	stopChan            chan struct{}        // channel to indicate whether we should stop
	keyGenQueuedMsgs    chan QueuedMsg       // messages queued up before local party is even ready
	keySignQueuedMsgs   chan QueuedMsg       // messages queued up for key sign
	broadcastChannel    chan *WrappedMessage // channel we used to broadcast message to other parties
	stateLock           *sync.Mutex
	tssLock             *sync.Mutex
	priKey              cryptokey.PrivKey
	preParams           *keygen.LocalPreParams
	homeBase            string
	unConfirmedMsgLock  *sync.Mutex
	unConfirmedMessages map[string]*LocalCacheItem
	partyIDtoP2PID      map[string]peer.ID
}

// NewTss create a new instance of Tss
func NewTss(bootstrapPeers []maddr.Multiaddr, p2pPort, tssPort int, priKeyBytes []byte, baseFolder string, optionalPreParams ...keygen.LocalPreParams) (*Tss, error) {
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
	// When using the keygen party it is recommended that you pre-compute the "safe primes" and Paillier secret beforehand because this can take some time.
	// This code will generate those parameters using a concurrency limit equal to the number of available CPU cores.
	var preParams *keygen.LocalPreParams
	if !byPassGeneratePreParam {
		if len(optionalPreParams) > 0 {
			preParams = &optionalPreParams[0]
		} else {
			preParams, err = keygen.GeneratePreParams(1 * time.Minute)
			if nil != err {
				return nil, fmt.Errorf("fail to generate pre parameters: %w", err)
			}
		}
	}

	t := &Tss{
		comm:                c,
		logger:              log.With().Str("module", "tss").Logger(),
		port:                tssPort,
		stopChan:            make(chan struct{}),
		partyLock:           &sync.Mutex{},
		keyGenQueuedMsgs:    make(chan QueuedMsg, 1024),
		keySignQueuedMsgs:   make(chan QueuedMsg, 1024),
		broadcastChannel:    make(chan *WrappedMessage),
		stateLock:           &sync.Mutex{},
		tssLock:             &sync.Mutex{},
		priKey:              priKey,
		preParams:           preParams,
		homeBase:            baseFolder,
		unConfirmedMsgLock:  &sync.Mutex{},
		unConfirmedMessages: make(map[string]*LocalCacheItem),
		partyIDtoP2PID:      make(map[string]peer.ID),
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

func getPriKeyRawBytes(priKey cryptokey.PrivKey) ([]byte, error) {
	var keyBytesArray [32]byte
	pk, ok := priKey.(secp256k1.PrivKeySecp256k1)
	if !ok {
		return nil, errors.New("private key is not secp256p1.PrivKeySecp256k1")
	}
	copy(keyBytesArray[:], pk[:])
	return keyBytesArray[:], nil
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *Tss) newHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", http.HandlerFunc(t.ping)).Methods(http.MethodGet)
	router.Handle("/keygen", http.HandlerFunc(t.keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keysign)).Methods(http.MethodPost)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pID)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *Tss) getP2pID(w http.ResponseWriter, _ *http.Request) {
	localPeerID := t.comm.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}

func (t *Tss) getParties(keys []string, localPartyKey string, keygen bool) ([]*btss.PartyID, *btss.PartyID, error) {
	var localPartyID *btss.PartyID
	var unSortedPartiesID []*btss.PartyID
	sort.Strings(keys)
	for idx, item := range keys {
		pk, err := sdk.GetAccPubKeyBech32(item)
		if nil != err {
			return nil, nil, fmt.Errorf("fail to get account pub key address(%s): %w", item, err)
		}
		secpPk := pk.(secp256k1.PubKeySecp256k1)
		key := new(big.Int).SetBytes(secpPk[:])
		// Set up the parameters
		// Note: The `id` and `moniker` fields are for convenience to allow you to easily track participants.
		// The `id` should be a unique string representing this party in the network and `moniker` can be anything (even left blank).
		// The `uniqueKey` is a unique identifying key for this peer (such as its p2p public key) as a big.Int.
		partyID := btss.NewPartyID(strconv.Itoa(idx), "", key)
		if item == localPartyKey {
			localPartyID = partyID
		}
		unSortedPartiesID = append(unSortedPartiesID, partyID)
	}
	if localPartyID == nil {
		return nil, nil, fmt.Errorf("local party is not in the list")
	}

	partiesID := btss.SortPartyIDs(unSortedPartiesID)
	// select the node on the "partiesID" rather than on the "keys" as the secret shares are sorted on the "index",
	// not on the node ID.
	if !keygen {
		threshold, err := getThreshold(len(keys))
		if nil != err {
			return nil, nil, err
		}
		partiesID = partiesID[:threshold+1]
	}

	return partiesID, localPartyID, nil
}

// emptyQueuedMessages
func (t *Tss) emptyQueuedMessages(input <-chan QueuedMsg) {
	t.logger.Debug().Msg("empty queue messages")
	defer t.logger.Debug().Msg("finished empty queue messages")
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	t.unConfirmedMessages = make(map[string]*LocalCacheItem)
	if len(input) == 0 {
		return
	}
	for {
		select {
		case m := <-input:
			t.logger.Debug().Msgf("drop queued message from %s", m.wrappedMsg.MessageType)
		default:
			return
		}
	}
}

func (t *Tss) getAllPartyPeerIDs() ([]peer.ID, error) {
	var result []peer.ID
	PartyInfo := t.getPartyInfo()
	if nil == PartyInfo {
		return nil, fmt.Errorf("fail to get keygen info")
	}
	for _, item := range PartyInfo.PartyIDMap {
		peerID, err := getPeerIDFromPartyID(item)
		if nil != err {
			return nil, fmt.Errorf("fail to get peer id from pub key")
		}
		result = append(result, peerID)
	}
	return result, nil
}

func getPeerIDFromPartyID(partyID *btss.PartyID) (peer.ID, error) {
	pkBytes := partyID.KeyInt().Bytes()
	var pk secp256k1.PubKeySecp256k1
	copy(pk[:], pkBytes)
	return getPeerIDFromSecp256PubKey(pk)
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
	localFileName := fmt.Sprintf("localstate-%d-%s.json", t.port, pubKey)
	if len(t.homeBase) > 0 {
		localFileName = filepath.Join(t.homeBase, localFileName)
	}
	return SaveLocalStateToFile(localFileName, keyGenLocalStateItem)

}

func (t *Tss) setPartyInfo(partyInfo *PartyInfo) {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	t.partyInfo = partyInfo
}

func (t *Tss) getPartyInfo() *PartyInfo {
	t.partyLock.Lock()
	defer t.partyLock.Unlock()
	return t.partyInfo
}

func (t *Tss) processQueuedMessages(input <-chan QueuedMsg) {
	t.logger.Debug().Msg("process queued messages")
	defer t.logger.Debug().Msg("finished processing queued messages")
	if len(input) == 0 {
		return
	}
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return
	}
	for {
		select {
		case m := <-input:
			t.logger.Debug().Msgf("process queued %s message", m.wrappedMsg.MessageType)
			if err := t.processOneMessage(m.wrappedMsg, m.peerID); nil != err {
				t.logger.Error().Err(err).Msg("fail to process a message from local queue")
			}
		default:
			return
		}
	}
}

func bytesToHashString(msg []byte) (string, error) {
	h := sha256.New()
	_, err := h.Write(msg)
	if nil != err {
		return "", fmt.Errorf("fail to caculate sha256 hash: %w", err)
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}

func (t *Tss) ping(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
	return
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
	prikeyBytes, err := getPriKeyRawBytes(t.priKey)
	if nil != err {
		return err
	}
	if err := t.comm.Start(prikeyBytes); nil != err {
		return fmt.Errorf("fail to start p2p communication layer: %w", err)
	}

	t.wg.Add(1)
	go t.processComm()
	t.wg.Add(1)
	go t.processBroadcastChannel()
	err = t.server.ListenAndServe()
	if err != nil && err != http.ErrServerClosed {
		log.Error().Err(err).Int("port", t.port).Msg("Failed to start the HTTP server")
		return err
	}
	t.wg.Wait()
	log.Info().Int("port", t.port).Msg("The HTTP server has been stopped successfully")
	return nil
}

func (t *Tss) processBroadcastChannel() {
	t.logger.Info().Msg("start to process broadcast message channel")
	defer t.logger.Info().Msg("stop process broadcast message channel")
	defer t.wg.Done()
	for {
		select {
		case <-t.stopChan:
			return // time to stop
		case msg := <-t.broadcastChannel:
			wireMsgBytes, err := json.Marshal(msg)
			if nil != err {
				t.logger.Error().Err(err).Msg("fail to marshal a wrapped message to json bytes")
				continue
			}
			peerIDs, err := t.getAllPartyPeerIDs()
			if nil != err {
				t.logger.Error().Err(err).Msg("fail to get all partys peer ids")
			}
			t.logger.Debug().Msgf("broadcast message %s to %+v", msg.MessageType, peerIDs)
			if err := t.comm.Broadcast(peerIDs, wireMsgBytes); nil != err {
				t.logger.Error().Err(err).Msg("fail to broadcast confirm message to all other parties")
			}
		}
	}
}

// processComm is
func (t *Tss) processComm() {
	t.logger.Info().Msg("start to process messages coming from communication channels")
	defer t.logger.Info().Msg("stop processing messages from communication channels")
	defer t.wg.Done()
	for {
		select {
		case <-t.stopChan:
			return // time to stop
		case m := <-t.comm.messages:
			var wrappedMsg WrappedMessage
			if err := json.Unmarshal(m.Payload, &wrappedMsg); nil != err {
				t.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}
			if err := t.processOneMessage(&wrappedMsg, m.PeerID.String()); nil != err {
				t.logger.Error().Err(err).Msg("fail to process message")
			}
		}
	}
}

// updateLocal will apply the wireMsg to local keygen/keysign party
func (t *Tss) updateLocal(wireMsg *WireMessage) error {
	if nil == wireMsg {
		t.logger.Warn().Msg("wire msg is nil")
	}
	partyInfo := t.getPartyInfo()
	if partyInfo == nil {
		return nil
	}
	partyID, ok := partyInfo.PartyIDMap[wireMsg.Routing.From.Id]
	if !ok {
		return fmt.Errorf("get message from unknown party %s", partyID.Id)
	}
	if _, err := partyInfo.Party.UpdateFromBytes(wireMsg.Message, partyID, wireMsg.Routing.IsBroadcast); nil != err {
		return fmt.Errorf("fail to set bytes to local party: %w", err)
	}
	return nil
}

func (t *Tss) isLocalPartyReady() bool {
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return false
	}
	return true
}

func (t *Tss) checkDupAndUpdateVerMsg(bMsg *BroadcastConfirmMessage, peerID string) bool {
	localCacheItem := t.tryGetLocalCacheItem(bMsg.Key)
	//we check whether this node has already sent the VerMsg message to avoid eclipse of others VerMsg
	if localCacheItem == nil {
		bMsg.P2PID = peerID
		return true
	}

	defer localCacheItem.lock.Unlock()
	localCacheItem.lock.Lock()
	if _, ok := localCacheItem.ConfirmedList[peerID]; ok {
		return false
	}
	bMsg.P2PID = peerID
	return true
}

func (t *Tss) processOneMessage(wrappedMsg *WrappedMessage, peerID string) error {
	t.logger.Debug().Msg("start process one message")
	defer t.logger.Debug().Msg("finish processing one message")
	if nil == wrappedMsg {
		return errors.New("invalid wireMessage")
	}
	if !t.isLocalPartyReady() {
		// local part is not ready , the tss node might not receive keygen request yet, Let's queue the message
		t.logger.Debug().Msgf("local party is not ready,queue it,%s", wrappedMsg.MessageType)
		switch wrappedMsg.MessageType {
		case TSSKeySignMsg, TSSKeySignVerMsg:
			t.keySignQueuedMsgs <- newQueuedMsg(wrappedMsg, peerID)
		case TSSKeyGenMsg, TSSKeyGenVerMsg:
			t.keyGenQueuedMsgs <- newQueuedMsg(wrappedMsg, peerID)
		}
		return nil
	}
	switch wrappedMsg.MessageType {
	case TSSKeyGenMsg, TSSKeySignMsg:
		var wireMsg WireMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &wireMsg); nil != err {
			return fmt.Errorf("fail to unmarshal wire message: %w", err)
		}
		return t.processTSSMsg(&wireMsg, wrappedMsg.MessageType)
	case TSSKeyGenVerMsg, TSSKeySignVerMsg:
		var bMsg BroadcastConfirmMessage
		if err := json.Unmarshal(wrappedMsg.Payload, &bMsg); nil != err {
			return fmt.Errorf("fail to unmarshal broadcast confirm message")
		}
		//we check whether this peer has already send us the VerMsg before update
		ret := t.checkDupAndUpdateVerMsg(&bMsg, peerID)
		if ret {
			return t.processVerMsg(&bMsg)
		}
		return nil

	}
	return nil
}

func (t *Tss) hashCheck(localCacheItem *LocalCacheItem) (string, error) {
	dataOwner := localCacheItem.Msg.Routing.From
	dataOwnerP2PID, ok := t.partyIDtoP2PID[dataOwner.Id]
	if !ok {
		t.logger.Warn().Msgf("error in find the data Owner P2PID\n")
		return dataOwnerP2PID.String(), errors.New("error in find the data Owner P2PID")
	}
	localCacheItem.lock.Lock()
	defer localCacheItem.lock.Unlock()

	targetHashValue := localCacheItem.Hash
	for P2PID, hashValue := range localCacheItem.ConfirmedList {
		if P2PID == dataOwnerP2PID.String() {
			t.logger.Warn().Msgf("we detect that the data owner try to send the hash for his own message\n")
			delete(localCacheItem.ConfirmedList, P2PID)
			return "", HashFromOwnerErr
		}
		if targetHashValue == hashValue {
			continue
		}
		t.logger.Error().Msgf("hash is not in consistency!!")
		return P2PID, errors.New("hash is not in consistency")
	}
	return "", nil
}

func (t *Tss) processOutCh(msg btss.Message, msgType THORChainTSSMessageType) error {
	buf, r, err := msg.WireBytes()
	// if we cannot get the wire share, the tss keygen will fail, we just quit.
	if nil != err {
		return fmt.Errorf("fail to get wire bytes: %w", err)
	}
	wireMsg := WireMessage{
		Routing:   r,
		RoundInfo: msg.Type(),
		Message:   buf,
	}
	wireMsgBytes, err := json.Marshal(wireMsg)
	if nil != err {
		return fmt.Errorf("fail to convert tss msg to wire bytes: %w", err)
	}
	wrappedMsg := &WrappedMessage{
		MessageType: msgType,
		Payload:     wireMsgBytes,
	}
	wrappedMsgBytes, err := json.Marshal(wrappedMsg)
	if nil != err {
		return fmt.Errorf("fail to marshal wrapped message to bytes: %w", err)
	}
	peerIDs := make([]peer.ID, 0)
	for _, each := range r.To {
		peerID, ok := t.partyIDtoP2PID[each.Id]
		if !ok {
			t.logger.Error().Msgf("error in find the P2P ID")
			continue
		}
		peerIDs = append(peerIDs, peerID)
	}
	if nil == peerIDs {
		t.logger.Debug().Msgf("broad cast msg to everyone from :%s ", r.From.Id)
	} else {
		t.logger.Debug().Msgf("sending message to (%v) from :%s", peerIDs, r.From.Id)
	}
	if err := t.comm.Broadcast(peerIDs, wrappedMsgBytes); nil != err {
		t.logger.Error().Err(err).Msg("fail to broadcast messages")
	}
	switch msgType {
	case TSSKeyGenMsg:
		t.processQueuedMessages(t.keyGenQueuedMsgs)
	case TSSKeySignMsg:
		t.processQueuedMessages(t.keySignQueuedMsgs)
	default:
		return fmt.Errorf("unknow message type")

	}
	return nil
}

func (t *Tss) processVerMsg(broadcastConfirmMsg *BroadcastConfirmMessage) error {
	t.logger.Debug().Msg("process ver msg")
	defer t.logger.Debug().Msg("finish process ver msg")
	if nil == broadcastConfirmMsg {
		return nil
	}
	partyInfo := t.getPartyInfo()
	if nil == partyInfo {
		return errors.New("can't process ver msg , local party is not ready")
	}
	key := broadcastConfirmMsg.Key
	localCacheItem := t.tryGetLocalCacheItem(key)
	if nil == localCacheItem {
		// we didn't receive the TSS Message yet
		localCacheItem = &LocalCacheItem{
			Msg:           nil,
			Hash:          broadcastConfirmMsg.Hash,
			lock:          &sync.Mutex{},
			ConfirmedList: make(map[string]string),
		}
		t.updateLocalUnconfirmedMessages(key, localCacheItem)
	}

	localCacheItem.UpdateConfirmList(broadcastConfirmMsg.P2PID, broadcastConfirmMsg.Hash)
	t.logger.Info().Msgf("total confirmed parties:%+v", localCacheItem.ConfirmedList)
	if localCacheItem.TotalConfirmParty() == (len(partyInfo.PartyIDMap)-1) && localCacheItem.Msg != nil {
		msg, err := t.hashCheck(localCacheItem)
		if nil != err {
			if err == HashFromOwnerErr {
				return nil
			}
			t.logger.Error().Msgf("The consistency check fail of node %s\n", msg)
			return err
		}

		if err := t.updateLocal(localCacheItem.Msg); nil != err {
			return fmt.Errorf("fail to update the message to local party: %w", err)
		}
		// the information had been confirmed by all party , we don't need it anymore
		t.logger.Debug().Msgf("remove key: %s", key)
		t.removeKey(key)
	}
	return nil
}

// processTSSMsg
func (t *Tss) processTSSMsg(wireMsg *WireMessage, msgType THORChainTSSMessageType) error {
	t.logger.Debug().Msg("process wire message")
	defer t.logger.Debug().Msg("finish process wire message")
	// we only update it local party
	if !wireMsg.Routing.IsBroadcast {
		t.logger.Debug().Msgf("msg from %s to %+v", wireMsg.Routing.From, wireMsg.Routing.To)
		return t.updateLocal(wireMsg)
	}
	// broadcast message , we save a copy locally , and then tell all others what we got
	msgHash, err := bytesToHashString(wireMsg.Message)
	if nil != err {
		return fmt.Errorf("fail to calculate hash of the wire message: %w", err)
	}
	partyInfo := t.getPartyInfo()
	key := wireMsg.GetCacheKey()
	//P2PID will be filled up by the receiver.
	broadcastConfirmMsg := &BroadcastConfirmMessage{
		P2PID: "",
		Key:   key,
		Hash:  msgHash,
	}
	localCacheItem := t.tryGetLocalCacheItem(key)
	if nil == localCacheItem {
		t.logger.Debug().Msgf("++%s doesn't exist yet,add a new one", key)
		localCacheItem = &LocalCacheItem{
			Msg:           wireMsg,
			Hash:          msgHash,
			lock:          &sync.Mutex{},
			ConfirmedList: make(map[string]string),
		}
		t.updateLocalUnconfirmedMessages(key, localCacheItem)
	} else {
		// this means we received the broadcast confirm message from other party first
		t.logger.Debug().Msgf("==%s exist", key)
		if localCacheItem.Msg == nil {
			t.logger.Debug().Msgf("==%s exist, set message", key)
			localCacheItem.Msg = wireMsg
			localCacheItem.Hash = msgHash
		}
	}
	localP2PID := t.comm.GetLocalPeerID()
	localCacheItem.UpdateConfirmList(localP2PID, msgHash)
	if localCacheItem.TotalConfirmParty() == (len(partyInfo.PartyIDMap) - 1) {
		if err := t.updateLocal(localCacheItem.Msg); nil != err {
			return fmt.Errorf("fail to update the message to local party: %w", err)
		}
	}
	buf, err := json.Marshal(broadcastConfirmMsg)
	if nil != err {
		return fmt.Errorf("fail to marshal borad cast confirm message: %w", err)
	}
	t.logger.Debug().Msg("broadcast VerMsg to all other parties")

	select {
	case t.broadcastChannel <- &WrappedMessage{
		MessageType: getBroadcastMessageType(msgType),
		Payload:     buf,
	}:
		return nil
	case <-t.stopChan:
		// time to stop
		return nil
	}
}
func getBroadcastMessageType(msgType THORChainTSSMessageType) THORChainTSSMessageType {
	switch msgType {
	case TSSKeyGenMsg:
		return TSSKeyGenVerMsg
	case TSSKeySignMsg:
		return TSSKeySignVerMsg
	default:
		return Unknown // this should not happen
	}

}

func (t *Tss) tryGetLocalCacheItem(key string) *LocalCacheItem {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	localCacheItem, ok := t.unConfirmedMessages[key]
	if !ok {
		return nil
	}
	return localCacheItem
}

func (t *Tss) updateLocalUnconfirmedMessages(key string, cacheItem *LocalCacheItem) {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	t.unConfirmedMessages[key] = cacheItem
}

func (t *Tss) removeKey(key string) {
	t.unConfirmedMsgLock.Lock()
	defer t.unConfirmedMsgLock.Unlock()
	delete(t.unConfirmedMessages, key)
}
