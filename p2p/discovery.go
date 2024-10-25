package p2p

import (
	"context"
	"fmt"
	"io"
	"strings"
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
const GossipInterval = 5 * time.Second

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
			fmt.Printf("Failed to connect to bootstrap peer %s: %s\n", pinfo.ID, err)
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
	pd.logger.Info().Msgf("Received discovery stream from %s", s.Conn().RemotePeer())
	defer s.Close()

	ma := s.Conn().RemoteMultiaddr()
	ai, err := peer.AddrInfoFromP2pAddr(ma)
	if err != nil {
		fmt.Printf("Failed to parse peer address(%s): %s\n", ma, err)
		return
	}
	pd.addPeer(*ai)

	// Share our known peers
	peers := pd.GetPeers()
	for _, p := range peers {
		// Send peer info (implement your own serialization format)
		// This is a simplified example
		addrStr := p.Addrs[0].String() + "," + p.ID.String() + "\n"
		_, err = s.Write([]byte(addrStr))
		if err != nil {
			pd.logger.Error().Err(err).Msgf("Failed to write to stream")
		}
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
	pd.logger.Info().Msgf("Gossiping known peers")
	peers := pd.GetPeers()
	pd.logger.Info().Msgf("current peers: %v", peers)

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
		pd.logger.Info().Msgf("Connected to peer %s", p)

		// Open discovery stream
		s, err := pd.host.NewStream(ctx, p.ID, DiscoveryProtocol)
		if err != nil {
			fmt.Printf("Failed to open discovery stream to %s: %s\n", p.ID, err)
			continue
		}
		pd.logger.Info().Msgf("Opened discovery stream to %s", p)

		// Read peer info from stream
		// This is a simplified example - implement proper serialization
		buf, err := io.ReadAll(s)
		if err != nil {
			s.Close()
			fmt.Printf("Failed to read from stream: %s\n", err)
			continue
		}

		// Parse received peer info and add to known peers
		peerData := string(buf)
		pd.logger.Info().Msgf("Received peer data: %s", peerData)
		for _, line := range strings.Split(peerData, "\n") {
			pd.logger.Info().Msgf("read line: %s", line)
			if line == "" {
				continue
			}
			parts := strings.Split(line, ",")
			if len(parts) != 2 {
				continue
			}

			addr, err := multiaddr.NewMultiaddr(parts[0])
			if err != nil {
				continue
			}

			id, err := peer.Decode(parts[1])
			if err != nil {
				continue
			}

			pd.addPeer(peer.AddrInfo{
				ID:    id,
				Addrs: []multiaddr.Multiaddr{addr},
			})
		}
		s.Close()
	}
}
