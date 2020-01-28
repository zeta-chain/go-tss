package tss

import (
	"context"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"time"

	"github.com/binance-chain/go-sdk/common/types"
	bkeygen "github.com/binance-chain/tss-lib/ecdsa/keygen"
	"github.com/binance-chain/tss-lib/ecdsa/signing"
	btss "github.com/binance-chain/tss-lib/tss"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/gorilla/mux"
	"github.com/libp2p/go-libp2p-core/protocol"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
	cryptokey "github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/secp256k1"
	"golang.org/x/sync/errgroup"

	"gitlab.com/thorchain/thornode/cmd"

	"gitlab.com/thorchain/tss/go-tss/common"
	"gitlab.com/thorchain/tss/go-tss/keygen"
	"gitlab.com/thorchain/tss/go-tss/keysign"
	"gitlab.com/thorchain/tss/go-tss/p2p"
)

var (
	ByPassGeneratePreParam = false
)

// PartyInfo the information used by tss key gen and key sign
type PartyInfo struct {
	Party      btss.Party
	PartyIDMap map[string]*btss.PartyID
}

type TssServer struct {
	conf             common.TssConfig
	logger           zerolog.Logger
	Status           common.TssStatus
	tssHttpServer    *http.Server
	infoHttpServer   *http.Server
	p2pCommunication *p2p.Communication
	priKey           cryptokey.PrivKey
	preParams        *bkeygen.LocalPreParams
	wg               sync.WaitGroup
	tssKeyGenLocker  *sync.Mutex
	tssKeySignLocker *sync.Mutex
	stopChan         chan struct{}
	subscribers      map[string]chan *p2p.Message
	homeBase         string
}

// NewHandler registers the API routes and returns a new HTTP handler
func (t *TssServer) tssNewHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/keygen", http.HandlerFunc(t.keygen)).Methods(http.MethodPost)
	router.Handle("/keysign", http.HandlerFunc(t.keySign)).Methods(http.MethodPost)
	router.Handle("/nodestatus", http.HandlerFunc(t.getNodeStatus)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

func (t *TssServer) infoHandler(verbose bool) http.Handler {
	router := mux.NewRouter()
	router.Handle("/ping", http.HandlerFunc(t.ping)).Methods(http.MethodGet)
	router.Handle("/p2pid", http.HandlerFunc(t.getP2pID)).Methods(http.MethodGet)
	router.Use(logMiddleware(verbose))
	return router
}

// Tssport should only listen to the loopback
func NewTssHttpServer(tssAddr string, t *TssServer) *http.Server {
	server := &http.Server{
		Addr:         tssAddr,
		Handler:      t.tssNewHandler(true),
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return server
}

func NewInfoHttpServer(infoAddr string, t *TssServer) *http.Server {
	server := &http.Server{
		Addr:         infoAddr,
		Handler:      t.infoHandler(true),
		ReadTimeout:  time.Second * 10,
		WriteTimeout: time.Second * 10,
	}
	return server
}

// NewTss create a new instance of Tss
func NewTss(bootstrapPeers []maddr.Multiaddr, p2pPort int, tssAddr, infoAddr string, protocolID protocol.ID, priKeyBytes []byte, rendezvous, baseFolder string, conf common.TssConfig) (*TssServer, error) {
	return newTss(bootstrapPeers, p2pPort, tssAddr, infoAddr, protocolID, priKeyBytes, rendezvous, baseFolder, conf)
}

// NewTss create a new instance of Tss
func newTss(bootstrapPeers []maddr.Multiaddr, p2pPort int, tssAddr, infoAddr string, protocolID protocol.ID, priKeyBytes []byte, rendezvous, baseFolder string, conf common.TssConfig, optionalPreParams ...bkeygen.LocalPreParams) (*TssServer, error) {
	priKey, err := getPriKey(string(priKeyBytes))
	if err != nil {
		return nil, errors.New("cannot parse the private key")
	}
	P2PServer, err := p2p.NewCommunication(rendezvous, bootstrapPeers, p2pPort, protocolID)
	if nil != err {
		return nil, fmt.Errorf("fail to create communication layer: %w", err)
	}
	setupBech32Prefix()
	// When using the keygen party it is recommended that you pre-compute the "safe primes" and Paillier secret beforehand because this can take some time.
	// This code will generate those parameters using a concurrency limit equal to the number of available CPU cores.
	var preParams *bkeygen.LocalPreParams
	if !ByPassGeneratePreParam {
		if len(optionalPreParams) > 0 {
			preParams = &optionalPreParams[0]
		} else {
			preParams, err = bkeygen.GeneratePreParams(conf.PreParamTimeout)
			if nil != err {
				return nil, fmt.Errorf("fail to generate pre parameters: %w", err)
			}
		}
	}
	if preParams == nil && !ByPassGeneratePreParam {
		return nil, errors.New("invalid ppreparams")
	}
	tssServer := TssServer{
		conf:   conf,
		logger: log.With().Str("module", "tss").Logger(),
		Status: common.TssStatus{
			Starttime: time.Now(),
		},
		p2pCommunication: P2PServer,
		priKey:           priKey,
		preParams:        preParams,
		tssKeyGenLocker:  &sync.Mutex{},
		tssKeySignLocker: &sync.Mutex{},
		stopChan:         make(chan struct{}),
		subscribers:      make(map[string]chan *p2p.Message),
		homeBase:         baseFolder,
	}
	tssServer.tssHttpServer = NewTssHttpServer(tssAddr, &tssServer)
	tssServer.infoHttpServer = NewInfoHttpServer(infoAddr, &tssServer)

	return &tssServer, nil
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

func StopServer(server *http.Server) error {
	c, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	err := server.Shutdown(c)
	if err != nil {
		log.Error().Err(err).Msg("Failed to shutdown the Tss server gracefully")
	}
	return err
}

func (t *TssServer) StartHttpServers() error {
	defer t.wg.Done()
	ctx := context.Background()
	g, newCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		err := t.tssHttpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			return err
		}
		return nil
	})
	g.Go(func() error {
		err := t.infoHttpServer.ListenAndServe()
		if err != nil && err != http.ErrServerClosed {
			log.Error().Err(err).Msg("Failed to start info HTTP server")
			return err
		}
		return nil
	})
	g.Go(func() error {
		select {
		case <-t.stopChan:
		case <-newCtx.Done():
		}
		err := StopServer(t.tssHttpServer)
		err2 := StopServer(t.infoHttpServer)
		if err != nil || err2 != nil {
			log.Error().Err(err).Msg("Failed to shutdown the Tss or info server gracefully")
			return errors.New("error in shutdown gracefully")
		}
		return nil
	})
	return g.Wait()

}

// Start Tss server
func (t *TssServer) Start(ctx context.Context) error {
	log.Info().Msg("Starting the HTTP servers")
	t.Status.Starttime = time.Now()
	t.wg.Add(1)
	go func() {
		<-ctx.Done()
		close(t.stopChan)
		//stop the p2p and finish the p2p wait group
		err := t.p2pCommunication.Stop()
		if err != nil {
			t.logger.Error().Msgf("error in shutdown the p2p server")
		}
	}()

	prikeyBytes, err := getPriKeyRawBytes(t.priKey)
	if nil != err {
		return err
	}

	go t.p2pCommunication.ProcessBroadcast()
	if err := t.p2pCommunication.Start(prikeyBytes); nil != err {
		return fmt.Errorf("fail to start p2p communication layer: %w", err)
	}
	err = t.StartHttpServers()
	if err != nil {
		return err
	}
	t.wg.Wait()
	log.Info().Msg("The Tss and p2p server has been stopped successfully")
	return nil
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

func (t *TssServer) ping(w http.ResponseWriter, _ *http.Request) {
	w.WriteHeader(http.StatusOK)
}

func (t *TssServer) keygen(w http.ResponseWriter, r *http.Request) {
	t.tssKeyGenLocker.Lock()
	defer t.tssKeyGenLocker.Unlock()
	status := common.Success
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
	var keygenReq keygen.KeyGenReq
	if err := decoder.Decode(&keygenReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode keygen request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}
	keygenInstance := keygen.NewTssKeyGen(t.homeBase, t.p2pCommunication.GetLocalPeerID(), t.conf, t.priKey, t.p2pCommunication.BroadcastMsgChan, &t.stopChan, t.preParams, &t.Status.CurrKeyGen)
	keygenMsgChannel, keygenSyncChannel := keygenInstance.GetTssKeyGenChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenMsg, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenVerMsg, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeyGenSync, keygenSyncChannel)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenMsg)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenVerMsg)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeyGenSync)

	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	k, err := keygenInstance.GenerateNewKey(keygenReq)
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keygen")
		atomic.AddUint64(&t.Status.FailedKeyGen, 1)
	} else {
		atomic.AddUint64(&t.Status.SucKeyGen, 1)
	}

	newPubKey, addr, err := common.GetTssPubKey(k)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to generate the new Tss key")
		status = common.Fail
	}
	if os.Getenv("NET") == "testnet" || os.Getenv("NET") == "mocknet" {
		types.Network = types.TestNetwork
	}
	resp := keygen.KeyGenResp{
		PubKey:      newPubKey,
		PoolAddress: addr.String(),
		Status:      status,
		Blame:       keygenInstance.GetTssCommonStruct().BlamePeers,
	}
	buf, err := json.Marshal(resp)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(buf)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}

}

func (t *TssServer) keySign(w http.ResponseWriter, r *http.Request) {
	t.tssKeySignLocker.Lock()
	defer t.tssKeySignLocker.Unlock()
	var keySignReq keysign.KeySignReq
	keySignFlag := common.Success
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
	if err := decoder.Decode(&keySignReq); nil != err {
		t.logger.Error().Err(err).Msg("fail to decode key sign request")
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	keysignInstance := keysign.NewTssKeySign(t.homeBase, t.p2pCommunication.GetLocalPeerID(), t.conf, t.priKey, t.p2pCommunication.BroadcastMsgChan, &t.stopChan, &t.Status.CurrKeySign)

	keygenMsgChannel, keygenSyncChannel := keysignInstance.GetTssKeySignChannels()
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignMsg, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignVerMsg, keygenMsgChannel)
	t.p2pCommunication.SetSubscribe(p2p.TSSKeySignSync, keygenSyncChannel)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignMsg)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignVerMsg)
	defer t.p2pCommunication.CancelSubscribe(p2p.TSSKeySignSync)

	signatureData, err := keysignInstance.SignMessage(keySignReq)
	// the statistic of keygen only care about Tss it self, even if the following http response aborts,
	// it still counted as a successful keygen as the Tss model runs successfully.
	if err != nil {
		t.logger.Error().Err(err).Msg("err in keysign")
		atomic.AddUint64(&t.Status.FailedKeySign, 1)
		keySignFlag = common.Fail
		signatureData = &signing.SignatureData{}
	} else {
		atomic.AddUint64(&t.Status.SucKeySign, 1)
	}
	// this indicates we are not in this round keysign
	if signatureData == nil && err == nil {
		keysignInstance.WriteKeySignResult(w, "", "", common.NA)
		return
	}
	keysignInstance.WriteKeySignResult(w, base64.StdEncoding.EncodeToString(signatureData.R), base64.StdEncoding.EncodeToString(signatureData.S), keySignFlag)
}

func (t *TssServer) getP2pID(w http.ResponseWriter, _ *http.Request) {
	localPeerID := t.p2pCommunication.GetLocalPeerID()
	_, err := w.Write([]byte(localPeerID))
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
func (t *TssServer) getNodeStatus(w http.ResponseWriter, _ *http.Request) {
	buf, err := json.Marshal(t.Status)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to marshal response to json")
		w.WriteHeader(http.StatusInternalServerError)
		return
	}
	_, err = w.Write(buf)
	if nil != err {
		t.logger.Error().Err(err).Msg("fail to write to response")
	}
}
