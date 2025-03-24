package p2p

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	libp2p "github.com/libp2p/go-libp2p"
	"github.com/libp2p/go-libp2p/core/crypto"
	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	rcmgr "github.com/libp2p/go-libp2p/p2p/host/resource-manager"
	"github.com/libp2p/go-libp2p/p2p/net/connmgr"
	"github.com/libp2p/go-libp2p/p2p/protocol/ping"
	maddr "github.com/multiformats/go-multiaddr"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"

	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
)

var joinPartyProtocolWithLeader protocol.ID = "/p2p/join-party-leader"

// TSSProtocolID protocol id used for tss
var TSSProtocolID protocol.ID = "/p2p/tss"

const (
	// TimeoutConnecting maximum time for wait for peers to connect
	TimeoutConnecting = time.Second * 20
)

// Message that get transfer across the wire
type Message struct {
	PeerID  peer.ID
	Payload []byte
}

// Communication use p2p to broadcast messages among all the TSS nodes
type Communication struct {
	bootstrapPeers   []maddr.Multiaddr
	logger           zerolog.Logger
	listenAddr       maddr.Multiaddr
	host             host.Host
	wg               *sync.WaitGroup
	stopChan         chan struct{} // channel to indicate whether we should stop
	subscribers      map[messages.THORChainTSSMessageType]*MessageIDSubscriber
	subscriberLocker *sync.Mutex
	streamCount      int64
	BroadcastMsgChan chan *messages.BroadcastMsgChan
	externalAddr     maddr.Multiaddr
	streamMgr        *StreamMgr
	whitelistedPeers []peer.ID
}

// NewCommunication create a new instance of Communication
func NewCommunication(
	bootstrapPeers []maddr.Multiaddr,
	port int,
	externalIP string,
	whitelistedPeers []peer.ID,
	logger zerolog.Logger,
) (*Communication, error) {
	listenTo := fmt.Sprintf("/ip4/0.0.0.0/tcp/%d", port)

	addr, err := maddr.NewMultiaddr(listenTo)
	if err != nil {
		return nil, errors.Wrap(err, listenTo)
	}

	externalAddr := maddr.Multiaddr(nil)

	if len(externalIP) != 0 {
		myself := fmt.Sprintf("/ip4/%s/tcp/%d", externalIP, port)
		externalAddr, err = maddr.NewMultiaddr(myself)
		if err != nil {
			return nil, errors.Wrapf(err, "external addr %q", myself)
		}
	}

	return &Communication{
		bootstrapPeers:   bootstrapPeers,
		logger:           logger.With().Str(logs.Component, "communication").Logger(),
		listenAddr:       addr,
		wg:               &sync.WaitGroup{},
		stopChan:         make(chan struct{}),
		subscribers:      make(map[messages.THORChainTSSMessageType]*MessageIDSubscriber),
		subscriberLocker: &sync.Mutex{},
		streamCount:      0,
		BroadcastMsgChan: make(chan *messages.BroadcastMsgChan, 1024),
		externalAddr:     externalAddr,
		streamMgr:        NewStreamMgr(logger),
		whitelistedPeers: whitelistedPeers,
	}, nil
}

// GetHost return the host
func (c *Communication) GetHost() host.Host {
	return c.host
}

// GetLocalPeerID from p2p host
func (c *Communication) GetLocalPeerID() string {
	return c.host.ID().String()
}

// Broadcast message to Peers
func (c *Communication) Broadcast(peers []peer.ID, msg []byte, msgID string) {
	if len(peers) == 0 {
		return
	}
	// try to discover all peers and then broadcast the messages
	c.wg.Add(1)
	go c.broadcastToPeers(peers, msg, msgID)
}

func (c *Communication) broadcastToPeers(peers []peer.ID, msg []byte, msgID string) {
	defer c.wg.Done()
	defer func() {
		c.logger.Debug().Msgf("finished sending message to peer(%v)", peers)
	}()
	var wgSend sync.WaitGroup
	wgSend.Add(len(peers))
	for _, p := range peers {
		go func(p peer.ID) {
			defer wgSend.Done()
			if err := c.writeToStream(p, msg, msgID); nil != err {
				c.logger.Error().Err(err).
					Stringer(logs.Peer, p).
					Str(logs.MsgID, msgID).
					Msg("Unable to broadcast message to peer")
			}
		}(p)
	}
	wgSend.Wait()
}

func (c *Communication) writeToStream(pID peer.ID, msg []byte, msgID string) error {
	// don't send to ourselves
	if pID == c.host.ID() {
		return nil
	}

	stream, err := c.connectToOnePeer(pID)
	switch {
	case err != nil:
		return errors.Wrap(err, "connectToOnePeer failed")
	case stream == nil:
		return nil
	}

	// todo: why we need to add stream here?
	defer func() {
		c.streamMgr.AddStream(msgID, stream)
	}()

	return WriteStreamWithBuffer(msg, stream)
}

func (c *Communication) readFromStream(stream network.Stream) {
	peerID := stream.Conn().RemotePeer().String()
	c.logger.Debug().Str(logs.Peer, peerID).Msg("Reading from peer's stream")

	const timeout = 10 * time.Second

	payload, err := ReadStreamWithBuffer(stream)
	if err != nil {
		c.logger.Error().Err(err).Str(logs.Peer, peerID).Msg("Failed to read from stream")
		c.streamMgr.AddStream("UNKNOWN", stream)
		return
	}

	var wrappedMsg messages.WrappedMessage
	if err := json.Unmarshal(payload, &wrappedMsg); nil != err {
		c.logger.Error().Err(err).Msg("Failed to unmarshal WrappedMessage")
		c.streamMgr.AddStream("UNKNOWN", stream)
		return
	}

	channel := c.getSubscriber(wrappedMsg.MessageType, wrappedMsg.MsgID)
	if nil == channel {
		c.logger.Debug().Msgf("no MsgID %s found for this message", wrappedMsg.MsgID)
		c.logger.Debug().Msgf("no MsgID %s found for this message", wrappedMsg.MessageType)
		_ = stream.Reset()
		return
	}

	c.streamMgr.AddStream(wrappedMsg.MsgID, stream)

	msg := &Message{
		PeerID:  stream.Conn().RemotePeer(),
		Payload: payload,
	}

	select {
	case channel <- msg:
		// all good, message sent
	case <-time.After(timeout):
		// Note that we aren't logging payload itself
		// as it might contain sensitive information
		c.logger.Warn().
			Str(logs.MsgID, wrappedMsg.MsgID).
			Str(logs.Peer, peerID).
			Str("protocol", string(stream.Protocol())).
			Str("message_type", wrappedMsg.MessageType.String()).
			Int("message_payload_bytes", len(wrappedMsg.Payload)).
			Float64("timeout", timeout.Seconds()).
			Msg("readFromStream: timeout to send message to channel")
	}
}

func (c *Communication) handleStream(stream network.Stream) {
	select {
	case <-c.stopChan:
		return
	default:
		c.readFromStream(stream)
	}
}

func (c *Communication) bootStrapConnectivityCheck() error {
	if len(c.bootstrapPeers) == 0 {
		c.logger.Error().Msg("No bootstrap node set, skip the connectivity check")
		return nil
	}

	var onlineNodes uint32
	var wg sync.WaitGroup
	for _, el := range c.bootstrapPeers {
		peer, err := peer.AddrInfoFromP2pAddr(el)
		if err != nil {
			c.logger.Error().Err(err).Msg("error in decode the bootstrap node, skip it")
			continue
		}
		wg.Add(1)
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second*2)
			defer cancel()
			defer wg.Done()
			outChan := ping.Ping(ctx, c.host, peer.ID)
			select {
			case ret, ok := <-outChan:
				if !ok {
					return
				}
				if ret.Error == nil {
					c.logger.Debug().Msgf("connect to peer %v with RTT %v", peer.ID, ret.RTT)
					atomic.AddUint32(&onlineNodes, 1)
				}
			case <-ctx.Done():
				c.logger.Error().Stringer(logs.Peer, peer.ID).Msg("fail to ping peer within 2 seconds")
			}
		}()
	}
	wg.Wait()

	if onlineNodes == 0 {
		return errors.New("cannot ping any bootstrap node")
	}

	c.logger.Info().Msgf("we have successfully ping pong %d nodes", onlineNodes)

	return nil
}

func (c *Communication) startChannel(privKeyBytes []byte) error {
	p2pPriKey, err := crypto.UnmarshalSecp256k1PrivateKey(privKeyBytes)
	if err != nil {
		return errors.Wrapf(err, "fail to unmarshal secp256k1 private key")
	}

	addressFactory := func(addrs []maddr.Multiaddr) []maddr.Multiaddr {
		if c.externalAddr != nil {
			return []maddr.Multiaddr{c.externalAddr}
		}

		return addrs
	}

	scalingLimits := rcmgr.DefaultLimits
	protocolPeerBaseLimit := rcmgr.BaseLimit{
		Streams:         4096,
		StreamsInbound:  2048,
		StreamsOutbound: 2048,
		Memory:          512 << 20,
	}
	protocolPeerLimitIncrease := rcmgr.BaseLimitIncrease{
		Streams:         512,
		StreamsInbound:  256,
		StreamsOutbound: 256,
		Memory:          64 << 20,
	}

	scalingLimits.ProtocolPeerBaseLimit = protocolPeerBaseLimit
	scalingLimits.ProtocolPeerLimitIncrease = protocolPeerLimitIncrease
	for _, item := range []protocol.ID{joinPartyProtocolWithLeader, TSSProtocolID} {
		scalingLimits.AddProtocolLimit(item, protocolPeerBaseLimit, protocolPeerLimitIncrease)
		scalingLimits.AddProtocolPeerLimit(item, protocolPeerBaseLimit, protocolPeerLimitIncrease)
	}

	// Add limits around included libp2p protocols
	libp2p.SetDefaultServiceLimits(&scalingLimits)

	// Turn the scaling limits into a static set of limits using `.AutoScale`. This
	// scales the limits proportional to your system memory.
	limits := scalingLimits.AutoScale()

	// The resource manager expects a limiter, se we create one from our limits.
	limiter := rcmgr.NewFixedLimiter(limits)

	m, err := rcmgr.NewResourceManager(
		limiter,
		rcmgr.WithAllowlistedMultiaddrs(c.bootstrapPeers),
		rcmgr.WithMetrics(NewResourceMetricReporter(c.logger)),
	)

	if err != nil {
		return errors.Wrapf(err, "fail to create resource manager")
	}

	cmgr, err := connmgr.NewConnManager(1024, 1500)
	if err != nil {
		return errors.Wrapf(err, "fail to create connection manager")
	}

	h, err := libp2p.New(libp2p.ListenAddrs([]maddr.Multiaddr{c.listenAddr}...),
		libp2p.Identity(p2pPriKey),
		libp2p.AddrsFactory(addressFactory),
		libp2p.ResourceManager(m),
		libp2p.ConnectionManager(cmgr),
		libp2p.ConnectionGater(NewWhitelistConnectionGater(c.whitelistedPeers, c.logger)),
		libp2p.DisableRelay(),
	)
	if err != nil {
		return errors.Wrapf(err, "fail to create p2p host")
	}

	c.host = h
	c.logger.Info().
		Stringer(logs.Host, h.ID()).
		Stringer("address", maddr.Join(h.Addrs()...)).
		Msgf("HOST CREATED")

	h.SetStreamHandler(TSSProtocolID, c.handleStream)

	for i := 0; i < 5; i++ {
		if err = c.connectToBootstrapPeers(); err == nil {
			// connected; proceed to connectivity check
			return c.bootStrapConnectivityCheck()
		}

		hasMoreAttempts := i < 4

		if hasMoreAttempts {
			c.logger.Error().Msg("Unable to connect to any bootstrap node, retry in 5 seconds")
			time.Sleep(time.Second * 5)
		}
	}

	return errors.Wrapf(err, "fail to connect to bootstrap peer")
}

func (c *Communication) connectToOnePeer(pid peer.ID) (network.Stream, error) {
	c.logger.Debug().Msgf("peer:%s,current:%s", pid, c.host.ID())

	// don't connect to itself
	if pid == c.host.ID() {
		return nil, nil
	}

	c.logger.Debug().Stringer(logs.Peer, pid).Msg("Connecting to peer")

	ctx, cancel := context.WithTimeout(context.Background(), TimeoutConnecting)
	defer cancel()

	stream, err := c.host.NewStream(ctx, pid, TSSProtocolID)
	if err != nil {
		return nil, errors.Wrapf(err, "unable to create %s stream to peer", TSSProtocolID)
	}

	return stream, nil
}

func (c *Communication) connectToBootstrapPeers() error {
	// Let's connect to the bootstrap nodes first. They will tell us about the
	// other nodes in the network.
	if len(c.bootstrapPeers) == 0 {
		c.logger.Info().Msg("no bootstrap node set, we skip the connection")
		return nil
	}

	var (
		errGroup     errgroup.Group
		hasConnected atomic.Bool
	)

	for _, peerAddr := range c.bootstrapPeers {
		pi, err := peer.AddrInfoFromP2pAddr(peerAddr)
		switch {
		case err != nil:
			return errors.Wrap(err, peerAddr.String())
		case pi.ID == c.host.ID():
			// noop
			continue
		}

		errGroup.Go(func() error {
			ctx, cancel := context.WithTimeout(context.Background(), TimeoutConnecting)
			defer cancel()

			if err := c.host.Connect(ctx, *pi); err != nil {
				c.logger.Error().Err(err).
					Stringer(logs.Peer, pi.ID).
					Stringer("peer_address", maddr.Join(pi.Addrs...)).
					Msg("Failed to connect to peer")

				return nil
			}

			c.logger.Info().
				Stringer(logs.Peer, pi.ID).
				Stringer("peer_address", maddr.Join(pi.Addrs...)).
				Msg("Connection established with bootstrap node")

			hasConnected.Store(true)

			return nil
		})
	}

	err := errGroup.Wait()
	switch {
	case err != nil:
		return err
	case !hasConnected.Load():
		return errors.New("fail to connect to any peer")
	}

	return nil
}

// Start will start the communication
func (c *Communication) Start(priKeyBytes []byte) error {
	err := c.startChannel(priKeyBytes)
	if err == nil {
		c.wg.Add(1)
		go c.ProcessBroadcast()
	}
	return err
}

// Stop communication
func (c *Communication) Stop() error {
	// we need to stop the handler and the p2p services firstly, then terminate the our communication threads
	if err := c.host.Close(); err != nil {
		c.logger.Err(err).Msg("fail to close host network")
	}

	close(c.stopChan)
	c.wg.Wait()
	return nil
}

func (c *Communication) SetSubscribe(topic messages.THORChainTSSMessageType, msgID string, channel chan *Message) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()

	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		messageIDSubscribers = NewMessageIDSubscriber()
		c.subscribers[topic] = messageIDSubscribers
	}
	messageIDSubscribers.Subscribe(msgID, channel)
}

func (c *Communication) getSubscriber(topic messages.THORChainTSSMessageType, msgID string) chan *Message {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()
	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		c.logger.Debug().Msgf("fail to find subscribers for %s", topic)
		return nil
	}
	return messageIDSubscribers.GetSubscriber(msgID)
}

func (c *Communication) CancelSubscribe(topic messages.THORChainTSSMessageType, msgID string) {
	c.subscriberLocker.Lock()
	defer c.subscriberLocker.Unlock()

	messageIDSubscribers, ok := c.subscribers[topic]
	if !ok {
		c.logger.Debug().Msgf("cannot find the given channels %s", topic.String())
		return
	}
	if nil == messageIDSubscribers {
		return
	}
	messageIDSubscribers.UnSubscribe(msgID)
	if messageIDSubscribers.IsEmpty() {
		delete(c.subscribers, topic)
	}
}

func (c *Communication) ProcessBroadcast() {
	c.logger.Debug().Msg("start to process broadcast message channel")
	defer c.logger.Debug().Msg("stop process broadcast message channel")
	defer c.wg.Done()
	for {
		select {
		case msg := <-c.BroadcastMsgChan:
			wrappedMsgBytes, err := json.Marshal(msg.WrappedMessage)
			if err != nil {
				c.logger.Error().Err(err).Msg("fail to marshal a wrapped message to json bytes")
				continue
			}
			c.logger.Debug().Msgf("broadcast message %s to %+v", msg.WrappedMessage, msg.PeersID)
			c.Broadcast(msg.PeersID, wrappedMsgBytes, msg.WrappedMessage.MsgID)

		case <-c.stopChan:
			return
		}
	}
}

func (c *Communication) ReleaseStream(msgID string) {
	c.streamMgr.ReleaseStream(msgID)
}
