package go_tss

import (
	"context"
	"encoding/binary"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	DefaultProtocolID = `tss`
	DefaultRendezvous = `Asgard`
)

// Communication use p2p to broadcast messages among all the TSS nodes
type Communication struct {
	Rendezvous       string // based on group
	bootstrapPeers   []maddr.Multiaddr
	logger           zerolog.Logger
	listenAddr       maddr.Multiaddr
	host             host.Host
	routingDiscovery *discovery.RoutingDiscovery
	wg               *sync.WaitGroup
	stopchan         chan struct{}
	streamLock       *sync.Mutex
	streams          map[string]network.Stream
	streamCount      int64
	messages         chan *Message
}

// NewCommunication create a new instance of Communication
func NewCommunication(rendezvous string, bootstrapPeers []maddr.Multiaddr, port int) (*Communication, error) {
	if len(rendezvous) == 0 {
		rendezvous = DefaultRendezvous
	}
	addr, err := maddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
	if nil != err {
		return nil, fmt.Errorf("fail to create listen addr: %w", err)
	}
	return &Communication{
		Rendezvous:     rendezvous,
		bootstrapPeers: bootstrapPeers,
		logger:         log.With().Str("module", "communication").Logger(),
		listenAddr:     addr,
		wg:             &sync.WaitGroup{},
		stopchan:       make(chan struct{}),
		streams:        make(map[string]network.Stream),
		streamLock:     &sync.Mutex{},
		streamCount:    0,
		messages:       make(chan *Message),
	}, nil
}

const LengthHeader = 4
const MaxPayload = 8192
const TimeoutInSecs = 10

// Broadcast message to Peers
func (c *Communication) Broadcast(peers []peer.ID, msg []byte) {
	c.wg.Add(1)
	go c.broadcastToPeers(peers, msg)
}

func (c *Communication) broadcastToPeers(peers []peer.ID, msg []byte) {
	defer c.wg.Done()
	for peerID, stream := range c.streams {
		shouldSend := false
		if len(peers) == 0 {
			shouldSend = true
		} else {
			for _, p := range peers {
				if p.String() == peerID {
					shouldSend = true
					break
				}
			}
		}
		if shouldSend {
			if err := c.writeToStream(stream, msg); nil != err {
				c.logger.Error().Err(err).Msg("fail to write to stream")
				if err := stream.Reset(); nil != err {
					c.logger.Error().Err(err).Msg("fail to close the stream")
				}
				delete(c.streams, peerID)
			}
		}
	}

}

func (c *Communication) writeToStream(stream network.Stream, msg []byte) error {
	c.logger.Debug().Msg("writing messages to stream")
	length := len(msg)
	buf := make([]byte, LengthHeader)
	binary.LittleEndian.PutUint32(buf, uint32(length))

	if err := stream.SetWriteDeadline(time.Now().Add(time.Second * TimeoutInSecs)); nil != err {
		return fmt.Errorf("fail to set write deadline")
	}
	n, err := stream.Write(buf)
	if nil != err {
		c.logger.Error().Err(err).Msgf("fail to write to peer : %s", stream.Conn().RemotePeer().String())
		return err
	}
	if n < LengthHeader {
		return fmt.Errorf("short write, we would like to write: %d, however we only write: %d", LengthHeader, n)
	}
	if err := stream.SetWriteDeadline(time.Now().Add(time.Second * TimeoutInSecs)); nil != err {
		return fmt.Errorf("fail to set write deadline")
	}
	n, err = stream.Write(msg)
	if nil != err {
		return fmt.Errorf("fail to write: %w", err)
	}
	if n < length {
		return fmt.Errorf("short write, we would like to write: %d, however we only write: %d", length, n)
	}
	return nil
}

func (c *Communication) readFromStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer().String()
	c.logger.Debug().Msgf("reading from stream of peer: %s", peerID)
	defer func() {
		if err := stream.Reset(); nil != err {
			c.logger.Error().Err(err).Msg("fail to close stream")
		}
	}()
	for {
		select {
		case <-c.stopchan:
			return
		default:
			// TODO read from the stream
			length := make([]byte, LengthHeader)
			n, err := stream.Read(length)
			if err != nil {
				c.logger.Error().Err(err).Msg("fail to read from stream")
				return
			}
			if n < LengthHeader {
				c.logger.Error().Msgf("short read, we only read :%d bytes", n)
				return
			}
			l := binary.LittleEndian.Uint32(length)
			// we are transferring protobuf messages , how big can that be , if it is larger then MaxPayload , then definitely no no...
			if l > MaxPayload {
				c.logger.Warn().Msgf("peer:%s trying to send %d bytes payload", peerID, l)
				return
			}
			buf := make([]byte, l)
			if err := stream.SetReadDeadline(time.Now().Add(time.Second * TimeoutInSecs)); nil != err {
				c.logger.Error().Err(err).Msg("fail to set read deadline")
			}
			n, err = stream.Read(buf)
			if nil != err {
				c.logger.Error().Err(err).Msg("fail to read from stream")
				return
			}
			if uint32(n) != l {
				// short reading
				c.logger.Error().Err(err).Msgf("we are expecting %d bytes , but we only got %d", l, n)
			}
			c.logger.Info().Msg(string(buf))
			// select {
			// case <-c.stopchan:
			// 	return
			// case c.messages <- &Message{
			// 	PeerID:  stream.Conn().RemotePeer(),
			// 	Payload: buf,
			// }:
			// }
		}
	}
}
func (c *Communication) handleStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer().String()
	c.streamLock.Lock()
	defer c.streamLock.Unlock()
	if _, ok := c.streams[peerID]; ok {
		return // we have that stream already
	}
	c.wg.Add(1)
	go c.readFromStream(stream)
	atomic.AddInt64(&c.streamCount, 1)
	c.streams[peerID] = stream
}

func (c *Communication) startChannel() error {
	ctx := context.Background()
	h, err := libp2p.New(ctx,
		libp2p.ListenAddrs([]maddr.Multiaddr{c.listenAddr}...),
	)
	if nil != err {
		return fmt.Errorf("fail to create p2p host: %w", err)
	}
	c.host = h
	c.logger.Info().Msgf("Host created, we are: %s, at: %s", h.ID(), h.Addrs())

	h.SetStreamHandler(DefaultProtocolID, c.handleStream)
	// Start a DHT, for use in peer discovery. We can't just make a new DHT
	// client because we want each peer to maintain its own local copy of the
	// DHT, so that the bootstrapping node of the DHT can go down without
	// inhibiting future peer discovery.
	kademliaDHT, err := dht.New(ctx, h)
	if err != nil {
		return fmt.Errorf("fail to create DHT: %w", err)
	}
	c.logger.Debug().Msg("Bootstrapping the DHT")
	if err = kademliaDHT.Bootstrap(ctx); err != nil {
		return fmt.Errorf("fail to bootstrap DHT: %w", err)
	}
	if err := c.connectToBootstrapPeers(); nil != err {
		return fmt.Errorf("fail to connect to bootstrap peer: %w", err)
	}
	// We use a rendezvous point "meet me here" to announce our location.
	// This is like telling your friends to meet you at the Eiffel Tower.

	routingDiscovery := discovery.NewRoutingDiscovery(kademliaDHT)
	discovery.Advertise(ctx, routingDiscovery, c.Rendezvous)
	c.routingDiscovery = routingDiscovery
	c.logger.Info().Msg("Successfully announced!")
	c.wg.Add(1)
	go c.continuouslySearchingForNewPeers()
	return nil
}

func (c *Communication) continuouslySearchingForNewPeers() {
	// Now, look for others who have announced
	// This is like your friend telling you the location to meet you.
	defer c.wg.Done()
	for {
		select {
		case <-c.stopchan:
			return
		default:
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute*5)
			peerChan, err := c.routingDiscovery.FindPeers(ctx, c.Rendezvous)
			if nil != err {
				c.logger.Error().Err(err).Msg("fail to find peers")
				cancel()
				continue
			}
			c.connectToPeers(peerChan)
			cancel()
		}
	}
}
func (c *Communication) connectToPeers(peerChan <-chan peer.AddrInfo) {
	for {
		select {
		case <-c.stopchan:
			return
		case ai, more := <-peerChan:
			if !more {
				return // no more
			}
			if err := c.connectToOnePeer(ai); nil != err {
				c.logger.Error().Err(err).Msg("fail to connect to peer")
			}
		}
	}
}
func (c *Communication) connectToOnePeer(ai peer.AddrInfo) error {
	c.logger.Info().Msgf("connect to peer : %s", ai.ID.String())
	// dont connect to itself
	if ai.ID == c.host.ID() {
		return nil
	}
	if _, ok := c.streams[ai.ID.String()]; ok {
		// we already connect to this stream
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*1)
	defer cancel()
	stream, err := c.host.NewStream(ctx, ai.ID, DefaultProtocolID)
	if nil != err {
		return fmt.Errorf("fail to create new stream to peer: %s, %w", ai.ID, err)
	}
	c.handleStream(stream)
	return nil
}
func (c *Communication) connectToBootstrapPeers() error {
	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	var wg sync.WaitGroup
	for _, peerAddr := range c.bootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(peerAddr)
		if nil != err {
			return fmt.Errorf("fail to add peer: %w", err)
		}
		wg.Add(1)
		go func() {
			defer wg.Done()
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			if err := c.host.Connect(ctx, *pi); err != nil {
				c.logger.Error().Err(err)
			}
			c.logger.Info().Msgf("Connection established with bootstrap node: %s", *pi)
		}()
	}
	wg.Wait()
	return nil
}

// Start will start the communication
func (c *Communication) Start() error {
	return c.startChannel()
}

// Stop
func (c *Communication) Stop() error {
	close(c.stopchan)
	c.wg.Wait()
	return nil
}
