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

const MaxGossipConcurrency = 50

var GossipInterval = 30 * time.Second

type PeerDiscovery struct {
	host           host.Host
	knownPeers     map[peer.ID]peer.AddrInfo
	bootstrapPeers []peer.AddrInfo
	mu             sync.RWMutex
	logger         zerolog.Logger
	closeChan      chan struct{}
}

func NewPeerDiscovery(h host.Host, bootstrapPeers []peer.AddrInfo) *PeerDiscovery {
	pd := &PeerDiscovery{
		host:           h,
		knownPeers:     make(map[peer.ID]peer.AddrInfo),
		bootstrapPeers: bootstrapPeers,
		logger:         log.With().Str("module", "peer-discovery").Logger(),
		closeChan:      make(chan struct{}),
	}

	// Set up discovery protocol handler
	h.SetStreamHandler(DiscoveryProtocol, pd.handleDiscovery)

	return pd
}

// Start begins the discovery process
func (pd *PeerDiscovery) Start(ctx context.Context) {
	pd.logger.Info().Msg("Starting peer discovery with bootstrap peers")
	// Connect to bootstrap peers first
	for _, pinfo := range pd.bootstrapPeers {
		if err := pd.host.Connect(ctx, pinfo); err != nil {
			pd.logger.Error().Err(err).
				Stringer("bootstrap_peer_id", pinfo.ID).
				Stringer("bootstrap_peer_info", pinfo).
				Msgf("Failed to connect to bootstrap peer")
			continue
		}
		pd.addPeer(pinfo)
	}

	//before periodic gossip, start two rounds of warmup; this is to ensure keygen/keysign unit test
	// success where there might not be enough time for gossip to propagate before the keygen starts.
	pd.gossipPeers(ctx)
	time.Sleep(1 * time.Second)
	pd.gossipPeers(ctx)
	// Start periodic gossip
	go pd.startGossip(ctx)
}

func (pd *PeerDiscovery) Stop() {
	close(pd.closeChan)
}

// addPeer adds a peer to known peers
func (pd *PeerDiscovery) addPeer(pinfo peer.AddrInfo) {
	pd.mu.Lock()
	defer pd.mu.Unlock()

	if pinfo.ID == pd.host.ID() {
		return // Don't add ourselves
	}
	oldPinfo, ok := pd.knownPeers[pinfo.ID]
	if ok {
		for _, addr := range pinfo.Addrs {
			if !multiaddr.Contains(oldPinfo.Addrs, addr) {
				oldPinfo.Addrs = append(oldPinfo.Addrs, addr)
			}
		}
	} else {
		oldPinfo = pinfo
	}
	pd.knownPeers[pinfo.ID] = oldPinfo
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
	pd.logger.Debug().
		Stringer("from_peer", s.Conn().RemotePeer()).
		Msgf("Received discovery stream")
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
		case _, ok := <-pd.closeChan:
			if !ok {
				pd.logger.Info().Msg("Peer discovery stopped")
				return
			}
			pd.logger.Warn().Msg("Should not receive from closed channel!")
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
	pd.logger.Debug().
		Array("peers", zerolog.Arr().Interface(peers)).
		Msgf("current peers")

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	var wg sync.WaitGroup
	sem := make(chan struct{}, MaxGossipConcurrency) // Limit concurrency

	for _, p := range peers {
		if p.ID == pd.host.ID() {
			continue
		}

		sem <- struct{}{}
		wg.Add(1)
		go func(p peer.AddrInfo) {
			defer wg.Done()
			defer func() { <-sem }()

			err := pd.host.Connect(ctx, p)
			if err != nil {
				pd.logger.Error().Err(err).
					Stringer("to", p.ID).
					Msg("Failed to connect to peer")
				return
			}
			pd.logger.Debug().
				Stringer("to", p).
				Msg("Connected to peer")

			// Open discovery stream
			s, err := pd.host.NewStream(ctx, p.ID, DiscoveryProtocol)
			if err != nil {
				pd.logger.Error().Err(err).
					Stringer("to", p).
					Msg("Failed to open discovery stream to peer")
				return
			}
			defer s.Close()
			pd.logger.Debug().
				Stringer("to", p).
				Msg("Opened discovery stream to peer")

			// Read peer info from stream
			// This is a simplified example - implement proper serialization
			limitedReader := io.LimitReader(s, 1<<20) // Limit to 1MB
			buf, err := io.ReadAll(limitedReader)
			if err != nil {
				pd.logger.Error().Err(err).
					Stringer("from", p).
					Msg("Failed to read from stream")
				return
			}
			pd.logger.Debug().Msgf("Received peer data: %s", string(buf))

			// Parse received peer info and add to known peers
			var recvPeers []peer.AddrInfo
			err = json.Unmarshal(buf, &recvPeers)
			if err != nil {
				pd.logger.Error().Err(err).
					Stringer("from", p).
					Msg("Failed to unmarshal peer data received")
				return
			}
			for _, p := range recvPeers {
				pd.logger.Debug().
					Stringer("peer", p).
					Msg("Adding peer")
				pd.addPeer(p)
			}
		}(p)
	}
	wg.Wait()
}
