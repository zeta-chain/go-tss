package p2p

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p-core/crypto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-libp2p-core/protocol"
	discovery "github.com/libp2p/go-libp2p-discovery"
	dht "github.com/libp2p/go-libp2p-kad-dht"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"
)

const (
	// LengthHeader represent how many bytes we used as header
	LengthHeader = 4
	// MaxPayload the maximum payload for a message
	MaxPayload = 81920 // 80kb
	// TimeoutInSecs maximum time to wait on read and write
	TimeoutInSecs = 10
	// TimeoutBroadcast maximum time to wait for message broadcast
	TimeoutBroadcast = 5
	// TimeoutConnecting maximum time for wait for peers to connect
	TimeoutConnecting = 1
)

// Message that get transfer across the wire
type Message struct {
	PeerID  peer.ID
	Payload []byte
}

// Communication use p2p to broadcast messages among all the TSS nodes
type Communication struct {
	rendezvous       string // based on group
	protocolID       protocol.ID
	bootstrapPeers   []maddr.Multiaddr
	logger           zerolog.Logger
	listenAddr       maddr.Multiaddr
	host             host.Host
	routingDiscovery *discovery.RoutingDiscovery
	wg               *sync.WaitGroup
	stopChan         chan struct{} // channel to indicate whether we should stop
	subscribers      map[THORChainTSSMessageType]chan *Message
	subscriberLocker *sync.Mutex
	streamCount      int64
	BroadcastMsgChan chan *BroadcastMsgChan
}

// NewCommunication create a new instance of Communication
func NewCommunication(rendezvous string, bootstrapPeers []maddr.Multiaddr, port int, protocolID protocol.ID) (*Communication, error) {
	addr, err := maddr.NewMultiaddr(fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port))
	if nil != err {
		return nil, fmt.Errorf("fail to create listen addr: %w", err)
	}
	return &Communication{
		rendezvous:       rendezvous,
		protocolID:       protocolID,
		bootstrapPeers:   bootstrapPeers,
		logger:           log.With().Str("module", "communication").Logger(),
		listenAddr:       addr,
		wg:               &sync.WaitGroup{},
		stopChan:         make(chan struct{}),
		subscribers:      make(map[THORChainTSSMessageType]chan *Message, 0),
		subscriberLocker: &sync.Mutex{},
		streamCount:      0,
		BroadcastMsgChan: make(chan *BroadcastMsgChan, 1024),
	}, nil
}

// GetLocalPeerID from p2p host
func (c *Communication) GetLocalPeerID() string {
	return c.host.ID().String()
}

// Broadcast message to Peers
func (c *Communication) Broadcast(peers []peer.ID, msg []byte) {
	// try to discover all peers and then broadcast the messages
	c.wg.Add(1)
	go c.broadcastToPeers(peers, msg)
}

func (c *Communication) broadcastToPeers(peers []peer.ID, msg []byte) {
	defer c.wg.Done()
	defer func() {
		c.logger.Debug().Msgf("finished sending message to peer(%v)", peers)
	}()
	if len(peers) == 0 {
		c.logger.Debug().Msgf("the peer list is empty")
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*TimeoutBroadcast)
	defer cancel()
	peerChan, err := c.routingDiscovery.FindPeers(ctx, c.rendezvous)
	if nil != err {
		c.logger.Error().Err(err).Msg("fail to find any peers")
		return
	}
	for {
		select {
		case <-c.stopChan:
			return // we need to stop the server
		case ai, more := <-peerChan:
			if !more {
				return
			}
			if c.shouldWeWriteToPeer(ai, peers) {
				if err := c.writeToStream(ai, msg); nil != err {
					c.logger.Error().Err(err).Msg("fail to write to stream")
				}
			}
		}
	}
}

func (c *Communication) shouldWeWriteToPeer(ai peer.AddrInfo, peers []peer.ID) bool {
	if len(peers) == 0 {
		// broadcast to everyone
		return true
	}
	for _, p := range peers {
		if ai.ID.String() == p.String() {
			return true
		}
	}
	return false
}
func (c *Communication) writeToStream(ai peer.AddrInfo, msg []byte) error {
	// don't send to ourself
	if ai.ID.String() == c.host.ID().String() {
		return nil
	}
	stream, err := c.connectToOnePeer(ai)
	if nil != err {
		return fmt.Errorf("fail to open stream to peer(%s): %w", ai.ID, err)
	}
	if nil == stream {
		return nil
	}

	defer func() {
		if err := stream.Close(); nil != err {
			c.logger.Error().Err(err).Msgf("fail to reset stream to peer(%s)", ai.ID)
		}
	}()
	c.logger.Debug().Msgf(">>>writing messages to peer(%s)", ai.ID)
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
		c.wg.Done()
		atomic.AddInt64(&c.streamCount, -1)
	}()
	for {
		select {
		case <-c.stopChan:
			return
		default:
			length := make([]byte, LengthHeader)
			// set read header timeout
			if err := stream.SetReadDeadline(time.Now().Add(time.Second * TimeoutInSecs)); nil != err {
				c.logger.Error().Err(err).Msgf("fail to set read header timeout,peerID:%s", peerID)
				return
			}
			n, err := stream.Read(length)
			if err != nil {
				if errors.Is(err, io.EOF) {
					return
				}
				c.logger.Error().Err(err).Msgf("fail to read from header from stream,peerID: %s", peerID)
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
				c.logger.Error().Err(err).Msgf("fail to read from stream,peerID: %s", peerID)
				return
			}
			if uint32(n) != l {
				// short reading
				c.logger.Error().Err(err).Msgf("we are expecting %d bytes , but we only got %d", l, n)
			}
			var wrappedMsg WrappedMessage
			if err := json.Unmarshal(buf, &wrappedMsg); nil != err {
				c.logger.Error().Err(err).Msg("fail to unmarshal wrapped message bytes")
				continue
			}
			channel, ok := c.subscribers[wrappedMsg.MessageType]
			if !ok {
				c.logger.Info().Msgf("no subscriber %s found for this message", wrappedMsg.MessageType.String())
				continue
			} else {
				channel <- &Message{
					PeerID:  stream.Conn().RemotePeer(),
					Payload: buf,
				}
			}

		}
	}
}
func (c *Communication) handleStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer().String()
	c.logger.Debug().Msgf("handle stream from peer: %s", peerID)
	c.wg.Add(1)
	// we will read from that stream
	go c.readFromStream(stream)
	atomic.AddInt64(&c.streamCount, 1)
}

func (c *Communication) startChannel(privKeyBytes []byte) error {
	ctx := context.Background()
	p2pPriKey, err := crypto.UnmarshalSecp256k1PrivateKey(privKeyBytes)
	if err != nil {
		c.logger.Error().Msgf("error is %f", err)
		return err
	}

	h, err := libp2p.New(ctx,
		libp2p.ListenAddrs([]maddr.Multiaddr{c.listenAddr}...),
		libp2p.Identity(p2pPriKey),
	)
	if nil != err {
		return fmt.Errorf("fail to create p2p host: %w", err)
	}
	c.host = h
	c.logger.Info().Msgf("Host created, we are: %s, at: %s", h.ID(), h.Addrs())
	h.SetStreamHandler(c.protocolID, c.handleStream)
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
	discovery.Advertise(ctx, routingDiscovery, c.rendezvous)
	c.routingDiscovery = routingDiscovery
	c.logger.Info().Msg("Successfully announced!")

	return nil
}

func (c *Communication) connectToOnePeer(ai peer.AddrInfo) (network.Stream, error) {
	c.logger.Debug().Msgf("peer:%s,current:%s", ai.ID, c.host.ID())
	// dont connect to itself
	if ai.ID == c.host.ID() {
		return nil, nil
	}
	c.logger.Debug().Msgf("connect to peer : %s", ai.ID.String())
	ctx, cancel := context.WithTimeout(context.Background(), time.Minute*TimeoutConnecting)
	defer cancel()
	stream, err := c.host.NewStream(ctx, ai.ID, c.protocolID)
	if nil != err {
		return nil, fmt.Errorf("fail to create new stream to peer: %s, %w", ai.ID, err)
	}
	return stream, nil
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
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute*TimeoutConnecting)
			defer cancel()
			if err := c.host.Connect(ctx, *pi); err != nil {
				c.logger.Error().Err(err)
				return
			}
			c.logger.Info().Msgf("Connection established with bootstrap node: %s", *pi)
		}()
	}
	wg.Wait()
	return nil
}

// Start will start the communication
func (c *Communication) Start(priKeyBytes []byte) error {
	return c.startChannel(priKeyBytes)
}

// Stop communication
func (c *Communication) Stop() error {
	//we need to stop the handler and the p2p services firstly, then terminate the our communication threads
	c.host.RemoveStreamHandler(c.protocolID)
	c.host.Close()
	close(c.stopChan)
	c.wg.Wait()
	return nil
}

func (c *Communication) SetSubscribe(topic THORChainTSSMessageType, channel chan *Message) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()
	c.subscribers[topic] = channel
}

func (c *Communication) CancelSubscribe(topic THORChainTSSMessageType) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()
	delete(c.subscribers, topic)
}

func (c *Communication) ProcessBroadcast() {
	c.logger.Info().Msg("start to process broadcast message channel")
	c.wg.Add(1)
	defer c.logger.Info().Msg("stop process broadcast message channel")
	defer c.wg.Done()
	for {
		select {
		case msg := <-c.BroadcastMsgChan:
			wrappedMsgBytes, err := json.Marshal(msg.WrappedMessage)
			if nil != err {
				c.logger.Error().Err(err).Msg("fail to marshal a wrapped message to json bytes")
				continue
			}
			c.logger.Debug().Msgf("broadcast message %s to %+v", msg.WrappedMessage, msg.PeersID)
			c.Broadcast(msg.PeersID, wrappedMsgBytes)

		case <-c.stopChan:
			return
		}
	}
}
