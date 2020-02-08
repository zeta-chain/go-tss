package p2p

import (
	"context"
	"errors"
	"fmt"
	"io/ioutil"
	"strings"
	"sync"
	"time"

	"github.com/gogo/protobuf/proto"
	"github.com/libp2p/go-libp2p-core/host"
	"github.com/libp2p/go-libp2p-core/network"
	"github.com/libp2p/go-libp2p-core/peer"
	"github.com/libp2p/go-yamux"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"gitlab.com/thorchain/tss/go-tss/messages"
)

const WaitForPartyGatheringTimeout time.Duration = time.Minute

type PartyCoordinator struct {
	logger       zerolog.Logger
	host         host.Host
	ceremonyLock *sync.Mutex
	ceremonies   map[string]*Ceremony
	wg           *sync.WaitGroup
	stopChan     chan struct{}
}

// NewPartyCoordinator create a new instance of PartyCoordinator
func NewPartyCoordinator(host host.Host) *PartyCoordinator {
	return &PartyCoordinator{
		logger:       log.With().Str("module", "party_coordinator").Logger(),
		host:         host,
		ceremonyLock: &sync.Mutex{},
		ceremonies:   make(map[string]*Ceremony),
		wg:           &sync.WaitGroup{},
		stopChan:     make(chan struct{}),
	}
}

// Start the party coordinator role
func (pc *PartyCoordinator) Start() {
	pc.logger.Info().Msg("start party coordinator")
	pc.wg.Add(1)
	go pc.ceremonyMonitor()
}

// Stop the PartyCoordinator rune
func (pc *PartyCoordinator) Stop() {
	defer pc.logger.Info().Msg("stop party coordinator")
	close(pc.stopChan)
	pc.wg.Wait()
}

// HandleStream handle party coordinate stream
func (pc *PartyCoordinator) HandleStream(stream network.Stream) {
	defer func() {
		if err := stream.Close(); err != nil {
			pc.logger.Err(err).Msg("fail to close the stream")
		}
	}()
	remotePeer := stream.Conn().RemotePeer()
	logger := pc.logger.With().Str("remote peer", remotePeer.String()).Logger()
	logger.Debug().Msg("reading from join party request")
	length, err := ReadLength(stream)
	if err != nil {
		logger.Err(err).Msg("fail to read length header from stream")
		return
	}
	payload, err := ReadPayload(stream, length)
	if err != nil {
		logger.Err(err).Msgf("fail to read payload from stream")
		return
	}
	var msg messages.JoinPartyRequest
	if err := proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal join party request")
		return
	}
	resp, err := pc.processJoinPartyRequest(remotePeer, &msg)
	if err != nil {
		logger.Error().Err(err).Msg("fail to process join party request")
		return
	}
	if err := pc.writeResponse(stream, resp); err != nil {
		logger.Error().Err(err).Msg("fail to write response to stream")
	}
}

func (pc *PartyCoordinator) processJoinPartyRequest(remotePeer peer.ID, msg *messages.JoinPartyRequest) (*messages.JoinPartyResponse, error) {
	joinParty := &JoinParty{
		Msg:  msg,
		Peer: remotePeer,
		Resp: make(chan *messages.JoinPartyResponse, 1),
	}
	if err := pc.onJoinParty(joinParty); err != nil {
		if errors.Is(err, errPartyGathered) {
			// join too late , ceremony already gather enough parties , and started already
			return &messages.JoinPartyResponse{
				ID:   msg.ID,
				Type: messages.JoinPartyResponse_AlreadyStarted,
			}, nil
		}
	}
	for {
		select {
		case r := <-joinParty.Resp:
			return r, nil
		case <-time.After(WaitForPartyGatheringTimeout):
			// TODO make this timeout dynamic based on the threshold
			if !pc.onJoinPartyTimeout(joinParty) {
				return &messages.JoinPartyResponse{
					ID:   msg.ID,
					Type: messages.JoinPartyResponse_Timeout,
				}, nil
			}
		}
	}
}

// writeResponse write the joinPartyResponse
func (pc *PartyCoordinator) writeResponse(stream network.Stream, resp *messages.JoinPartyResponse) error {
	buf, err := proto.Marshal(resp)
	if err != nil {
		return fmt.Errorf("fail to marshal resp to byte: %w", err)
	}
	_, err = stream.Write(buf)
	if err != nil {
		// when fail to write to the stream we shall reset it
		if resetErr := stream.Reset(); resetErr != nil {
			return fmt.Errorf("fail to reset the stream: %w", err)
		}
		return fmt.Errorf("fail to write response to stream: %w", err)
	}
	return nil
}

var (
	errPartyGathered = errors.New("ceremony party already assembled")
)

// onJoinParty is a call back function
func (pc *PartyCoordinator) onJoinParty(joinParty *JoinParty) error {
	pc.logger.Info().
		Str("ID", joinParty.Msg.ID).
		Str("remote peer", joinParty.Peer.String()).
		Int32("threshold", joinParty.Msg.Threshold).
		Str("peer ids", strings.Join(joinParty.Msg.PeerID, ",")).
		Msgf("get join party request")
	pc.ceremonyLock.Lock()
	defer pc.ceremonyLock.Unlock()
	c, ok := pc.ceremonies[joinParty.Msg.ID]
	if !ok {
		ceremony := &Ceremony{
			ID:        joinParty.Msg.ID,
			Threshold: uint32(joinParty.Msg.Threshold),
			JoinPartyRequests: []*JoinParty{
				joinParty,
			},
			Status:  GatheringParties,
			Started: time.Now().UTC(),
		}
		pc.ceremonies[joinParty.Msg.ID] = ceremony
		return nil
	}
	if c.Status == Finished {
		return errPartyGathered
	}

	c.JoinPartyRequests = append(c.JoinPartyRequests, joinParty)
	if !c.IsReady() {
		// Ceremony is not ready , still waiting for more party to join
		return nil
	}

	resp := &messages.JoinPartyResponse{
		ID:     c.ID,
		Type:   messages.JoinPartyResponse_Success,
		PeerID: c.GetParties(),
	}
	for _, item := range c.JoinPartyRequests {
		select {
		case <-pc.stopChan: // receive request to exit
			return nil
		case item.Resp <- resp:
		}
	}
	c.Status = Finished
	return nil
}

// onJoinPartyTimeout this method is to deal with the follow scenario
// the join party request had been waiting for a while(WaitForPartyGatheringTimeout)
// but it doesn't get enough nodes to start the ceremony , thus it trying to withdraw it's request
// the first bool return value indicate whether it should give up sending the timeout resp back to client
// usually that means a timeout and success has almost step on each other's foot, it should give up timeout , because the
// success resp is already there
func (pc *PartyCoordinator) onJoinPartyTimeout(joinParty *JoinParty) bool {
	pc.logger.Info().
		Str("ID", joinParty.Msg.ID).
		Str("remote peer", joinParty.Peer.String()).
		Int32("threshold", joinParty.Msg.Threshold).
		Str("peer ids", strings.Join(joinParty.Msg.PeerID, ",")).
		Msgf("join party timeout")
	pc.ceremonyLock.Lock()
	defer pc.ceremonyLock.Unlock()
	c, ok := pc.ceremonies[joinParty.Msg.ID]
	if !ok {
		return false
	}
	// it could be timeout / finish almost happen at the same time, we give up timeout
	if c.Status == Finished {
		return true
	}
	// remove this party as they sick of waiting
	idxToDelete := -1
	for idx, p := range c.JoinPartyRequests {
		if p.Peer == joinParty.Peer {
			idxToDelete = idx
		}
	}
	c.JoinPartyRequests = append(c.JoinPartyRequests[:idxToDelete], c.JoinPartyRequests[idxToDelete+1:]...)
	return false
}

func (pc *PartyCoordinator) ceremonyMonitor() {
	defer pc.wg.Done()
	for {
		select {
		case <-pc.stopChan:
			return
		case <-time.After(time.Second * 30):
			pc.markCeremony()
		}
	}
}

func (pc *PartyCoordinator) markCeremony() {
	pc.ceremonyLock.Lock()
	defer pc.ceremonyLock.Unlock()
	for key, c := range pc.ceremonies {
		if time.Since(c.Started) > (2 * WaitForPartyGatheringTimeout) {
			pc.logger.Info().
				Str("ID", key).
				Str("Status", c.Status.String()).
				Str("started", c.Started.String()).
				Msg("remove ceremony")
			delete(pc.ceremonies, key)
		}
	}
}

// JoinParty join a ceremony , it could be keygen or key sign
func (pc *PartyCoordinator) JoinParty(remotePeer peer.ID, msg *messages.JoinPartyRequest) (*messages.JoinPartyResponse, error) {
	if remotePeer == pc.host.ID() {
		return pc.processJoinPartyRequest(remotePeer, msg)
	}
	msgBuf, err := proto.Marshal(msg)
	if err != nil {
		return nil, fmt.Errorf("fail to marshal msg to bytes: %w", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*30)
	defer cancel()
	stream, err := pc.host.NewStream(ctx, remotePeer, joinPartyProtocol)
	if err != nil {
		return nil, fmt.Errorf("fail to create stream to peer(%s):%w", remotePeer, err)
	}
	pc.logger.Info().Msgf("open stream to (%s) successfully", remotePeer)
	defer func() {
		if err := stream.Close(); err != nil {
			pc.logger.Error().Err(err).Msg("fail to close stream")
		}
	}()
	if err := WriteLength(stream, uint32(len(msgBuf))); err != nil {
		return nil, err
	}
	_, err = stream.Write(msgBuf)
	if err != nil {
		if errReset := stream.Reset(); errReset != nil {
			return nil, errReset
		}
		return nil, fmt.Errorf("fail to write message to stream:%w", err)
	}
	// read response
	respBuf, err := ioutil.ReadAll(stream)
	if err != nil {
		if err != yamux.ErrConnectionReset {
			return nil, fmt.Errorf("fail to read response: %w", err)
		}
	}
	if len(respBuf) == 0 {
		return nil, errors.New("fail to get response")
	}
	var resp messages.JoinPartyResponse
	if err := proto.Unmarshal(respBuf, &resp); err != nil {
		return nil, fmt.Errorf("fail to unmarshal JoinGameResp: %w", err)
	}
	return &resp, nil
}
