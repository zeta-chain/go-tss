package tss

import (
	"sync"
	"time"

	bkeygen "github.com/bnb-chain/tss-lib/ecdsa/keygen"
	tcrypto "github.com/cometbft/cometbft/crypto"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types/bech32/legacybech32"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/peer"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"

	"github.com/zeta-chain/go-tss/common"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/keygen"
	"github.com/zeta-chain/go-tss/keysign"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/monitor"
	"github.com/zeta-chain/go-tss/p2p"
	"github.com/zeta-chain/go-tss/storage"
)

// IServer define the necessary functionality should be provide by a TSS Server implementation.
type IServer interface {
	Start() error
	Stop()
	GetLocalPeerID() string
	GetKnownPeers() []peer.AddrInfo
	Keygen(req keygen.Request) (keygen.Response, error)
	KeygenAllAlgo(req keygen.Request) ([]keygen.Response, error)
	KeySign(req keysign.Request) (keysign.Response, error)
}

// Server is the structure that can provide all keysign and key gen features
type Server struct {
	conf              common.TssConfig
	logger            zerolog.Logger
	p2pCommunication  *p2p.Communication
	localNodePubKey   string
	preParams         *bkeygen.LocalPreParams
	tssKeyGenLocker   *sync.Mutex
	stopChan          chan struct{}
	joinPartyChan     chan struct{}
	partyCoordinator  *p2p.PartyCoordinator
	stateManager      storage.LocalStateManager
	signatureNotifier *keysign.SignatureNotifier
	privateKey        tcrypto.PrivKey
	tssMetrics        *monitor.Metric
}

type PeerInfo struct {
	ID      string
	Address string
}

type NetworkConfig struct {
	common.TssConfig
	ExternalIP       string
	Port             int
	BootstrapPeers   []maddr.Multiaddr
	WhitelistedPeers []peer.ID
}

// New constructs Server.
func New(
	net NetworkConfig,
	baseFolder string,
	privateKey tcrypto.PrivKey,
	tssPassword string,
	preParams *bkeygen.LocalPreParams,
	logger zerolog.Logger,
) (*Server, error) {
	logger = logger.With().Str(logs.Module, "tss").Logger()

	privateKeyBytes, err := conversion.GetPriKeyRawBytes(privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "fail to get private key raw bytes")
	}

	pubKey, err := pubKeyBech32(privateKey)
	if err != nil {
		return nil, errors.Wrap(err, "fail to derive public key bech32")
	}

	preParams, err = ensurePreParams(preParams, net.PreParamTimeout)
	if err != nil {
		return nil, errors.Wrap(err, "fail to ensure pre-params")
	}

	stateManager, err := storage.NewFileStateMgr(baseFolder, tssPassword)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to create file state manager")
	}

	bootstrapPeers, err := resolveBootstrapPeers(net, stateManager)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to resolve bootstrap peers")
	}

	comm, err := p2p.NewCommunication(
		bootstrapPeers,
		net.Port,
		net.ExternalIP,
		net.WhitelistedPeers,
		logger,
	)
	if err != nil {
		return nil, errors.Wrapf(err, "fail to create communication layer")
	}

	if err := comm.Start(privateKeyBytes); nil != err {
		return nil, errors.Wrap(err, "unable to start p2p network")
	}

	sn := keysign.NewSignatureNotifier(comm.GetHost(), logger)
	sn.Start()

	metrics := monitor.NewMetric()
	if net.EnableMonitor {
		metrics.Enable()
	}

	return &Server{
		conf:              net.TssConfig,
		logger:            logger,
		p2pCommunication:  comm,
		localNodePubKey:   pubKey,
		preParams:         preParams,
		tssKeyGenLocker:   &sync.Mutex{},
		stopChan:          make(chan struct{}),
		partyCoordinator:  p2p.NewPartyCoordinator(comm.GetHost(), net.PartyTimeout, logger),
		stateManager:      stateManager,
		signatureNotifier: sn,
		privateKey:        privateKey,
		tssMetrics:        metrics,
	}, nil
}

// Start Tss server
func (t *Server) Start() error {
	t.logger.Info().Msg("starting the tss servers")
	return nil
}

// Stop Tss server
func (t *Server) Stop() {
	close(t.stopChan)
	// stop the p2p and finish the p2p wait group
	err := t.p2pCommunication.Stop()
	if err != nil {
		t.logger.Error().Msg("error in shutdown the p2p server")
	}
	t.partyCoordinator.Stop()
	t.signatureNotifier.Stop()
	t.logger.Info().Msg("The tss and p2p server has been stopped successfully")
}

// nolint:unused // used in tests
func (t *Server) setJoinPartyChan(jpc chan struct{}) {
	t.joinPartyChan = jpc
}

// nolint:unused // used in tests
func (t *Server) unsetJoinPartyChan() {
	t.joinPartyChan = nil
}

func (t *Server) notifyJoinPartyChan() {
	if t.joinPartyChan != nil {
		t.joinPartyChan <- struct{}{}
	}
}

func (t *Server) joinParty(
	msgID string,
	blockHeight int64,
	participants []string,
	threshold int,
	sigChan chan string,
) ([]peer.ID, peer.ID, error) {
	if len(participants) == 0 {
		return nil, "", errors.New("no participants can be found")
	}

	t.logger.Info().Str(logs.MsgID, msgID).Msg("We apply the join party with a leader")

	peersID, err := conversion.GetPeerIDsFromPubKeys(participants)
	if err != nil {
		return nil, "", errors.New("fail to convert the public key to peer ID")
	}

	return t.partyCoordinator.JoinPartyWithLeader(msgID, blockHeight, peersID, threshold, sigChan)
}

// GetLocalPeerID return the local peer
func (t *Server) GetLocalPeerID() string {
	return t.p2pCommunication.GetLocalPeerID()
}

// GetKnownPeers return the the ID and IP address of all peers.
func (t *Server) GetKnownPeers() []peer.AddrInfo {
	var (
		host        = t.p2pCommunication.GetHost()
		connections = host.Network().Conns()
		result      = []peer.AddrInfo{}
	)

	for _, conn := range connections {
		ai := peer.AddrInfo{
			ID:    conn.RemotePeer(),
			Addrs: []maddr.Multiaddr{conn.RemoteMultiaddr()},
		}

		result = append(result, ai)
	}

	return result
}

// GetP2PHost return the libp2p host of the Communicator inside Server
func (t *Server) GetP2PHost() host.Host {
	return t.p2pCommunication.GetHost()
}

func pubKeyBech32(privateKey tcrypto.PrivKey) (string, error) {
	pk := secp256k1.PubKey{
		Key: privateKey.PubKey().Bytes()[:],
	}

	pubKey, err := sdk.MarshalPubKey(sdk.AccPK, &pk)
	if err != nil {
		return "", err
	}

	return pubKey, nil
}

// resolveBootstrapPeers resolved bootstrap peers based on the whitelist & local state.
func resolveBootstrapPeers(net NetworkConfig, stateManager *storage.FileStateMgr) ([]maddr.Multiaddr, error) {
	// validate
	if len(net.WhitelistedPeers) == 0 {
		return nil, errors.New("whitelisted peers missing")
	}

	// Retrieve peers from local state, merge with bootstrap peers
	peers, err := stateManager.RetrieveP2PAddresses()
	if err != nil {
		peers = net.BootstrapPeers
	} else {
		peers = append(peers, net.BootstrapPeers...)
	}

	// Make a set of whitelisted peers
	whitelistSet := make(map[peer.ID]bool)
	for _, w := range net.WhitelistedPeers {
		whitelistSet[w] = true
	}

	var selectedPeers []maddr.Multiaddr
	for _, v := range peers {
		peer, err := peer.AddrInfoFromP2pAddr(v)
		if err != nil {
			return nil, errors.Wrapf(err, "fail to parse peer %q", v.String())
		}

		if whitelistSet[peer.ID] {
			selectedPeers = append(selectedPeers, v)
		}
	}

	// Deduplicate the result
	cache := make(map[string]struct{})
	deduped := make([]maddr.Multiaddr, 0, len(selectedPeers))

	for _, p := range selectedPeers {
		if _, ok := cache[p.String()]; ok {
			continue
		}

		deduped = append(deduped, p)
		cache[p.String()] = struct{}{}
	}

	return deduped, nil
}

// ensurePreParams when using the keygen party it is recommended that you pre-compute the
// "safe primes" and Paillier secret beforehand because this can take some
// time. This code will generate those parameters using a concurrency limit equal
// to the number of available CPU cores.
func ensurePreParams(preParams *bkeygen.LocalPreParams, timeout time.Duration) (*bkeygen.LocalPreParams, error) {
	// noop
	if preParams != nil && preParams.Validate() {
		return preParams, nil
	}

	preParams, err := bkeygen.GeneratePreParams(timeout)
	switch {
	case err != nil:
		return nil, errors.Wrap(err, "unable to generate pre-params")
	case !preParams.Validate():
		return nil, errors.New("invalid pre-params")
	}

	return preParams, nil
}
