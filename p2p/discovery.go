package p2p

import (
	"context"
	"encoding/json"
	"io"
	"sort"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
)

const (
	DiscoveryProtocol     = "/tss/discovery/1.0.0"
	DefaultGossipInterval = 30 * time.Second
)

const (
	maxGossipConcurrency = 50
	maxGossipTimeout     = 10 * time.Second
)

type PeerDiscovery struct {
	host           host.Host
	knownPeers     map[peer.ID]peer.AddrInfo
	bootstrapPeers []peer.AddrInfo
	gossipInterval time.Duration
	closeChan      chan struct{}
	mu             sync.RWMutex
	logger         zerolog.Logger
}

func NewPeerDiscovery(
	h host.Host,
	bootstrapPeers []peer.AddrInfo,
	gossipInterval time.Duration,
	logger zerolog.Logger,
) *PeerDiscovery {
	if gossipInterval == 0 {
		gossipInterval = DefaultGossipInterval
	}

	pd := &PeerDiscovery{
		host:           h,
		knownPeers:     make(map[peer.ID]peer.AddrInfo),
		bootstrapPeers: bootstrapPeers,
		closeChan:      make(chan struct{}),
		gossipInterval: gossipInterval,
		logger:         logger.With().Str("module", "peer_discovery").Logger(),
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

		pd.ensurePeer(pinfo)
	}

	// Start periodic gossip
	go pd.gossipWorker(ctx)
}

func (pd *PeerDiscovery) Stop() {
	close(pd.closeChan)
}

// ensurePeer ensures peer in knownPeers. Might update existing peer's addresses.
func (pd *PeerDiscovery) ensurePeer(remote peer.AddrInfo) {
	// noop
	if remote.ID == pd.host.ID() {
		return
	}

	pd.mu.Lock()
	defer pd.mu.Unlock()

	existing, ok := pd.knownPeers[remote.ID]

	// first time seeing this peer
	if !ok {
		pd.knownPeers[remote.ID] = remote
		return
	}

	// existing peer might have a new address
	for _, addr := range remote.Addrs {
		if !multiaddr.Contains(existing.Addrs, addr) {
			existing.Addrs = append(existing.Addrs, addr)
		}
	}

	pd.knownPeers[remote.ID] = existing
}

func (pd *PeerDiscovery) getKnownPeers() []peer.AddrInfo {
	pd.mu.RLock()
	defer pd.mu.RUnlock()

	peers := make([]peer.AddrInfo, 0, len(pd.knownPeers))
	for _, p := range pd.knownPeers {
		peers = append(peers, p)
	}

	// sort peers by id (in place) to make response deterministic
	sort.Slice(peers, func(a, b int) bool { return peers[a].ID < peers[b].ID })

	return peers
}

// handleDiscovery handles incoming discovery streams. How it works:
// - save remote peer info into known peers
// - share known peers with remote peer
func (pd *PeerDiscovery) handleDiscovery(s network.Stream) {
	lf := map[string]any{
		"stream.remote_peer_id":   s.Conn().RemotePeer().String(),
		"stream.remote_peer_addr": s.Conn().RemoteMultiaddr().String(),
		"stream.id":               s.ID(),
	}

	pd.logger.Debug().Fields(lf).Msg("Received PeerDiscovery.handleDiscovery stream")

	defer func() {
		if err := s.Close(); err != nil {
			pd.logger.Error().Err(err).Fields(lf).Msg("Unable to close PeerDiscovery.handleDiscovery stream")
			return
		}

		pd.logger.Debug().Fields(lf).Msg("Closed PeerDiscovery.handleDiscovery stream")
	}()

	// Add remote caller to known peers ...
	pd.ensurePeer(peer.AddrInfo{
		ID:    s.Conn().RemotePeer(),
		Addrs: []multiaddr.Multiaddr{s.Conn().RemoteMultiaddr()},
	})

	// ... and share known peers with remote caller
	response, err := json.Marshal(pd.getKnownPeers())
	if err != nil {
		pd.logger.Error().Err(err).Fields(lf).Msg("Failed to marshal peers")
		return
	}

	if _, err = s.Write(response); err != nil {
		pd.logger.Error().Err(err).Fields(lf).Msg("Failed to write to stream")
	}
}

// gossipWorker periodically gossips known peers with connected peers
func (pd *PeerDiscovery) gossipWorker(ctx context.Context) {
	// initial run
	pd.gossipPeers(ctx)

	ticker := time.NewTicker(pd.gossipInterval)
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
	peers := pd.getKnownPeers()

	pd.logger.Debug().
		Array("peers", zerolog.Arr().Interface(peers)).
		Msg("Gossiping known peers")

	ctx, cancel := context.WithTimeout(ctx, maxGossipTimeout)
	defer cancel()

	var errg errgroup.Group

	errg.SetLimit(maxGossipConcurrency)

	for i := range peers {
		remotePeer := peers[i]

		// skip self. should not happen, but let's be safe
		if pd.host.ID() == remotePeer.ID {
			continue
		}

		errg.Go(func() error {
			if err := pd.gossipPeer(ctx, remotePeer); err != nil {
				pd.logger.Error().Err(err).Msg("Failed to gossip peer")
			}

			return nil
		})
	}

	// none of the gossiping should fail, only log errors
	_ = errg.Wait()
}

func (pd *PeerDiscovery) gossipPeer(ctx context.Context, p peer.AddrInfo) (err error) {
	defer func() {
		if pn := recover(); pn != nil {
			err = errors.Errorf("panic during gossipPeer: %+v", pn)
		}
	}()

	if err = pd.host.Connect(ctx, p); err != nil {
		return errors.Wrap(err, "failed to connect to peer")
	}

	lf := map[string]any{
		"gossip.remote_peer_id":      p.ID.String(),
		"gossip.remote_peer_address": p.Addrs,
	}

	pd.logger.Debug().Fields(lf).Msg("Connected to peer")

	s, err := pd.host.NewStream(ctx, p.ID, DiscoveryProtocol)
	if err != nil {
		return errors.Wrap(err, "failed to open discovery stream to peer")
	}

	defer func() {
		if errClose := s.Close(); errClose != nil {
			pd.logger.Error().Err(errClose).Fields(lf).Msg("Unable to close gossip stream")
		}
	}()

	lf["gossip.stream_id"] = s.ID()
	pd.logger.Debug().Fields(lf).Msg("Opened discovery stream to peer")

	// Read peer info from stream
	// This is a simplified example - implement proper serialization
	limitedReader := io.LimitReader(s, 1<<20) // Limit to 1MB
	buf, err := io.ReadAll(limitedReader)
	if err != nil {
		return errors.Wrap(err, "failed to read from stream")
	}

	pd.logger.Debug().Bytes("gossip.response", buf).Msg("Received peer data")

	// Parse received peer info and add to known peers
	var recvPeers []peer.AddrInfo
	if err = json.Unmarshal(buf, &recvPeers); err != nil {
		return errors.Wrap(err, "failed to unmarshal peer data received")
	}

	for _, remotePeer := range recvPeers {
		pd.logger.Debug().
			Stringer("gossip.received_peer_id", p.ID).
			Interface("gossip.received_peer_address", p.Addrs).
			Msg("Ensuring peer")

		pd.ensurePeer(remotePeer)
	}

	return nil
}
