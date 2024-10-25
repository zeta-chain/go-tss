package p2p

import (
	"context"
	"encoding/json"
	"io"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const DiscoveryProtocol = "/tss/discovery/1.0.0"

var GossipInterval = 10 * time.Second

type PeerDiscovery struct {
	host           host.Host
	knownPeers     map[peer.ID]peer.AddrInfo
	bootstrapPeers []peer.AddrInfo
	mu             sync.RWMutex
	logger         zerolog.Logger
}

func NewPeerDiscovery(h host.Host, bootstrapPeers []peer.AddrInfo) *PeerDiscovery {
	pd := &PeerDiscovery{
		host:           h,
		knownPeers:     make(map[peer.ID]peer.AddrInfo),
		bootstrapPeers: bootstrapPeers,
		logger:         log.With().Str("module", "peer-discovery").Logger(),
	}

	// Set up discovery protocol handler
	h.SetStreamHandler(DiscoveryProtocol, pd.handleDiscovery)

	return pd
}

// Start begins the discovery process
func (pd *PeerDiscovery) Start(ctx context.Context) {
	pd.logger.Info().Msgf("Starting peer discovery with bootstrap peers: %v", pd.bootstrapPeers)
	// Connect to bootstrap peers first
	for _, pinfo := range pd.bootstrapPeers {
		if err := pd.host.Connect(ctx, pinfo); err != nil {
			pd.logger.Error().Err(err).Msgf("Failed to connect to bootstrap peer %s", pinfo.ID)
			continue
		}
		pd.addPeer(pinfo)
	}

	// Start periodic gossip
	go pd.startGossip(ctx)
}

// addPeer adds a peer to known peers
func (pd *PeerDiscovery) addPeer(pinfo peer.AddrInfo) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pinfo.ID == pd.host.ID() {
		return // Don't add ourselves
	}
	pd.knownPeers[pinfo.ID] = pinfo
}

// GetPeers returns all known peers
func (pd *PeerDiscovery) GetPeers() []peer.AddrInfo {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	peers := make([]peer.AddrInfo, 0, len(pd.knownPeers))
	for _, p := range pd.knownPeers {
		peers = append(peers, p)
	}
	return peers
}

// handleDiscovery handles incoming discovery streams
func (pd *PeerDiscovery) handleDiscovery(s network.Stream) {
	pd.logger.Debug().Msgf("Received discovery stream from %s", s.Conn().RemotePeer())
	defer s.Close()

	ma := s.Conn().RemoteMultiaddr()

	ai := peer.AddrInfo{
		ID:    s.Conn().RemotePeer(),
		Addrs: []multiaddr.Multiaddr{ma},
	}
	pd.addPeer(ai)

	// Share our known peers
	peers := pd.GetPeers()
	data, err := json.Marshal(peers)
	if err != nil {
		pd.logger.Error().Err(err).Msgf("Failed to marshal peers")
		return
	}
	_, err = s.Write(data)
	if err != nil {
		pd.logger.Error().Err(err).Msgf("Failed to write to stream")
	}
}

// startGossip periodically shares peer information
func (pd *PeerDiscovery) startGossip(ctx context.Context) {
	ticker := time.NewTicker(GossipInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:

			pd.gossipPeers(ctx)
		}
	}
}

func (pd *PeerDiscovery) gossipPeers(ctx context.Context) {
	pd.logger.Debug().Msgf("Gossiping known peers")
	peers := pd.GetPeers()
	pd.logger.Debug().Msgf("current peers: %v", peers)

	ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
	defer cancel()
	for _, p := range peers {
		if p.ID == pd.host.ID() {
			continue
		}

		err := pd.host.Connect(ctx, p)
		if err != nil {
			pd.logger.Error().Err(err).Msgf("Failed to connect to peer %s", p)
		}
		pd.logger.Debug().Msgf("Connected to peer %s", p)

		// Open discovery stream
		s, err := pd.host.NewStream(ctx, p.ID, DiscoveryProtocol)
		if err != nil {
			pd.logger.Error().Err(err).Msgf("Failed to open discovery stream to %s", p)
			continue
		}
		pd.logger.Debug().Msgf("Opened discovery stream to %s", p)

		// Read peer info from stream
		// This is a simplified example - implement proper serialization
		buf, err := io.ReadAll(s)
		if err != nil {
			s.Close()
			pd.logger.Error().Err(err).Msgf("Failed to read from stream")
			continue
		}
		pd.logger.Info().Msgf("Received peer data: %s", string(buf))

		// Parse received peer info and add to known peers
		var recvPeers []peer.AddrInfo
		err = json.Unmarshal(buf, &recvPeers)
		if err != nil {
			s.Close()
			pd.logger.Error().Err(err).Msgf("Failed to unmarshal peer data")
			continue
		}
		for _, p := range recvPeers {
			pd.logger.Debug().Msgf("Adding peer %s", p)
			pd.addPeer(p)
		}

		s.Close()
	}
}
