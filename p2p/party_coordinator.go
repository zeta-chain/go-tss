package p2p

import (
	"context"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/pkg/errors"
	"github.com/rs/zerolog"
	"golang.org/x/sync/errgroup"
	"google.golang.org/protobuf/proto"

	"github.com/zeta-chain/go-tss/config"
	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
)

const NotificationSigReceived = "signature received"

var (
	ErrJoinPartyTimeout = errors.New("fail to join party, timeout")
	ErrLeaderNotReady   = errors.New("leader not reachable")
	ErrSigReceived      = errors.New(NotificationSigReceived)
	ErrNotActiveSigner  = errors.New("not active signer")
	ErrSigGenerated     = errors.New("signature generated")
)

type PartyCoordinator struct {
	logger     zerolog.Logger
	host       host.Host
	stopChan   chan struct{}
	timeout    time.Duration
	peersGroup map[string]*peerStatus
	mu         *sync.Mutex
	streamMgr  *StreamManager
}

// NewPartyCoordinator create a new instance of PartyCoordinator
func NewPartyCoordinator(host host.Host, timeout time.Duration, logger zerolog.Logger) *PartyCoordinator {
	logger = logger.With().Str(logs.Component, "party_coordinator").Logger()

	// if no timeout is given, default to 10 seconds
	if timeout == 0 {
		timeout = 10 * time.Second
	}

	pc := &PartyCoordinator{
		logger:     logger,
		host:       host,
		stopChan:   make(chan struct{}),
		timeout:    timeout,
		peersGroup: make(map[string]*peerStatus),
		mu:         &sync.Mutex{},
		streamMgr:  NewStreamManager(logger, config.StreamExcessTTL),
	}

	host.SetStreamHandler(ProtocolJoinPartyWithLeader, pc.HandleStreamWithLeader)

	return pc
}

// Stop the PartyCoordinator rune
func (pc *PartyCoordinator) Stop() {
	defer pc.logger.Info().Msg("stopping party coordinator")
	pc.host.RemoveStreamHandler(ProtocolJoinPartyWithLeader)
	close(pc.stopChan)
}

func (pc *PartyCoordinator) processRespMsg(respMsg *messages.JoinPartyLeaderComm, stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()

	pc.mu.Lock()
	peerGroup, ok := pc.peersGroup[respMsg.ID]
	pc.mu.Unlock()

	if !ok {
		pc.logger.Info().
			Str(logs.MsgID, respMsg.ID).
			Stringer(logs.Peer, remotePeer).
			Msg("message id from peer can not be found")

		pc.streamMgr.ResetStream(respMsg.ID, stream)
		return
	}

	defer pc.streamMgr.Stash(respMsg.ID, stream)

	leaderID := peerGroup.getLeader()

	if remotePeer != leaderID {
		pc.logger.Warn().
			Stringer(logs.Peer, remotePeer).
			Stringer(logs.Leader, leaderID).
			Str(logs.MsgID, respMsg.ID).
			Msg("This party is not the leader as expected")

		return
	}

	peerGroup.setLeaderResponse(respMsg)
	peerGroup.notify <- true

	err := WriteStreamWithBuffer([]byte(ResponseMessage), stream)
	if err != nil {
		pc.logger.Error().Err(err).
			Stringer(logs.Peer, remotePeer).
			Stringer(logs.Leader, leaderID).
			Str(logs.MsgID, respMsg.ID).
			Msg("Failed to write the reply to peer")
	}
}

func (pc *PartyCoordinator) processReqMsg(requestMsg *messages.JoinPartyLeaderComm, stream network.Stream) {
	pc.mu.Lock()
	peerGroup, ok := pc.peersGroup[requestMsg.ID]
	pc.mu.Unlock()

	if !ok {
		pc.logger.Info().Str(logs.MsgID, requestMsg.ID).Msg("This party is not ready")
		pc.streamMgr.ResetStream(requestMsg.ID, stream)
		return
	}

	pc.streamMgr.Stash(requestMsg.ID, stream)

	remotePeer := stream.Conn().RemotePeer()
	partyFormed, err := peerGroup.updatePeer(remotePeer)
	if err != nil {
		pc.logger.Error().Err(err).Msg("receive msg from unknown peer")
		return
	}

	if partyFormed {
		peerGroup.notify <- true
	}
}

// HandleStream handle party coordinate stream
func (pc *PartyCoordinator) HandleStreamWithLeader(stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()

	logger := pc.logger.With().Stringer(logs.Peer, remotePeer).Logger()
	logger.Debug().Msg("reading from join party request")

	payload, err := ReadStreamWithBuffer(stream)
	if err != nil {
		logger.Err(err).Msg("fail to read payload from stream")
		pc.streamMgr.StashUnknown(stream)
		return
	}

	var msg messages.JoinPartyLeaderComm

	if err = proto.Unmarshal(payload, &msg); err != nil {
		logger.Err(err).Msg("fail to unmarshal party data")
		pc.streamMgr.StashUnknown(stream)
		return
	}

	logger.Debug().Msgf("received message type=%s", msg.MsgType)

	switch msg.MsgType {
	case msgTypeRequest:
		pc.processReqMsg(&msg, stream)
		return
	case msgTypeResponse:
		pc.processRespMsg(&msg, stream)
		return
	default:
		logger.Err(err).Msg("fail to process this message")
		pc.streamMgr.StashUnknown(stream)
		return
	}
}

func (pc *PartyCoordinator) RemovePeerGroup(messageID string) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	delete(pc.peersGroup, messageID)
}

func (pc *PartyCoordinator) createJoinPartyGroups(
	messageID string,
	leaderID peer.ID,
	peerIDs []peer.ID,
	threshold int,
) (*peerStatus, error) {
	pc.mu.Lock()
	defer pc.mu.Unlock()
	peerStatus := newPeerStatus(peerIDs, pc.host.ID(), leaderID, threshold)
	pc.peersGroup[messageID] = peerStatus
	return peerStatus, nil
}

// todo drop this function and use []peer.ID
func (pc *PartyCoordinator) getPeerIDs(ids []string) ([]peer.ID, error) {
	result := make([]peer.ID, len(ids))
	for i, item := range ids {
		pid, err := peer.Decode(item)
		if err != nil {
			return nil, errors.Wrapf(err, "failed to decode peer id %q", item)
		}
		result[i] = pid
	}

	return result, nil
}

func (pc *PartyCoordinator) sendResponseToAll(msg *messages.JoinPartyLeaderComm, peers []peer.ID) {
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		pc.logger.Error().Err(err).Any("msg", msg).Msg("sendResponseToAll: failed to marshall response")
		return
	}

	var errGroup errgroup.Group

	for i := range peers {
		peer := peers[i]
		if peer == pc.host.ID() {
			continue
		}

		errGroup.Go(func() error {
			err := pc.sendMsgToPeer(msg.ID, peer, msgSend, true)
			if err != nil {
				pc.logger.Error().Err(err).
					Str(logs.MsgID, msg.ID).
					Stringer(logs.Peer, peer).
					Msg("Unable to send JoinPartyLeaderComm to peer")
			}

			return nil
		})
	}

	_ = errGroup.Wait()
}

func (pc *PartyCoordinator) sendRequestToLeader(msg *messages.JoinPartyLeaderComm, leader peer.ID) error {
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		return errors.Wrap(err, "failed to marshall request")
	}

	if err := pc.sendMsgToPeer(msg.ID, leader, msgSend, false); err != nil {
		return errors.Wrap(err, "sendMsgToPeer")
	}

	return nil
}

func (pc *PartyCoordinator) sendMsgToPeer(msgID string, pid peer.ID, payload []byte, needResponse bool) error {
	const protoID = ProtocolJoinPartyWithLeader

	ctx, cancel := context.WithTimeout(context.Background(), config.StreamTimeoutConnect)
	defer cancel()

	pc.logger.Debug().Msgf("try to open stream to (%s) ", pid)

	stream, err := pc.host.NewStream(ctx, pid, protoID)
	if err != nil {
		return errors.Wrapf(err, "failed to create %s stream to peer %q", protoID, pid.String())
	}

	defer pc.streamMgr.Stash(msgID, stream)

	if err = WriteStreamWithBuffer(payload, stream); err != nil {
		return errors.Wrap(err, "failed to write message to opened stream")
	}

	if !needResponse {
		return nil
	}

	_, err = ReadStreamWithBuffer(stream)
	switch {
	case errors.Is(err, network.ErrReset):
		// expected case
	case err != nil:
		pc.logger.Error().Err(err).
			Str(logs.MsgID, msgID).
			Stringer(logs.Peer, pid).
			Msg("Failed to await the response from peer")
	}

	return nil
}

func (pc *PartyCoordinator) joinPartyMember(
	msgID string,
	peerGroup *peerStatus,
	sigChan chan string,
) ([]peer.ID, error) {
	leaderID := peerGroup.getLeader()
	msg := messages.JoinPartyLeaderComm{
		MsgType: msgTypeRequest,
		ID:      msgID,
	}

	lf := map[string]any{
		logs.MsgID:  msgID,
		logs.Leader: leaderID,
	}

	sendRequest := func() {
		if err := pc.sendRequestToLeader(&msg, leaderID); err != nil {
			pc.logger.Error().Err(err).Fields(lf).Msg("Request to the leader failed")
		}
	}

	wg := sync.WaitGroup{}
	done := make(chan struct{})

	wg.Add(1)
	go func() {
		defer wg.Done()

		ticker := time.NewTicker(config.PartyJoinMemberRetryInterval)
		defer ticker.Stop()

		// send the first request (to avoid initial ticker delay)
		sendRequest()

		for {
			select {
			case <-done:
				return
			case <-ticker.C:
				sendRequest()
			}
		}
	}()

	var sigNotify string

	// now we wait for the leader to notify us who we do the keygen/keysign with
	select {
	case <-pc.stopChan:
		// promptly tear down this goroutine if partyCoordinator is stopped
		pc.logger.Debug().Fields(lf).Msg("party coordinator stopped")
	case <-peerGroup.notify:
		pc.logger.Debug().Fields(lf).Msg("received a response from the leader")
	case <-time.After(pc.timeout):
		pc.logger.Debug().Msgf("timed out waiting for a response from the leader after %s", pc.timeout)
	case result := <-sigChan:
		pc.logger.Debug().Msgf("received %s from sigChan", result)
		sigNotify = result
	}

	close(done)
	wg.Wait()

	if sigNotify == NotificationSigReceived {
		return nil, ErrSigReceived
	}

	if peerGroup.getLeaderResponse() == nil {
		leaderPk, err := conversion.GetPubKeyFromPeerID(leaderID.String())
		if err != nil {
			pc.logger.Error().Err(err).Fields(lf).Msg("received no response from the leader")
		} else {
			lf["leader_pk"] = leaderPk
			pc.logger.Error().Fields(lf).Msg("received no response from the leader")
		}

		return nil, ErrLeaderNotReady
	}

	onlineNodes := peerGroup.getLeaderResponse().PeerIDs

	// we trust the returned nodes returned by the leader,
	// if tss fail, the leader also will get blamed.
	pIDs, err := pc.getPeerIDs(onlineNodes)

	switch {
	case err != nil:
		return nil, errors.Wrap(err, "failed to parse peer ids")
	case len(pIDs) < peerGroup.threshold:
		return pIDs, errors.New("not enough peers")
	case peerGroup.getLeaderResponse().Type == messages.JoinPartyLeaderComm_Success:
		return pIDs, nil
	default:
		return pIDs, errors.Wrapf(ErrJoinPartyTimeout, "(timeout: %s)", pc.timeout.String())
	}
}

func (pc *PartyCoordinator) joinPartyLeader(
	msgID string,
	peerGroup *peerStatus,
	sigChan chan string,
) ([]peer.ID, error) {
	var sigNotify string
	select {
	case <-pc.stopChan:
		// promptly tear down this goroutine if partyCoordinator is stopped
		pc.logger.Debug().Msg("leader's party coordinator stopped")
	case <-peerGroup.notify:
		pc.logger.Debug().Msg("we have enough participants")
	case <-time.After(pc.timeout / 2):
		// timeout, reporting to peers before their timeout
		pc.logger.Debug().Msgf("leader timed out waiting for peers after %s", pc.timeout/2)
	case result := <-sigChan:
		sigNotify = result
	}

	if sigNotify == NotificationSigReceived {
		return nil, ErrSigReceived
	}

	allPeers := peerGroup.getAllPeers()
	onlinePeers, _ := peerGroup.getPeersStatus()
	onlinePeers = append(onlinePeers, pc.host.ID())

	tssNodes := make([]string, len(onlinePeers))
	for i, el := range onlinePeers {
		tssNodes[i] = el.String()
	}

	msg := messages.JoinPartyLeaderComm{
		MsgType: msgTypeResponse,
		ID:      msgID,
		Type:    messages.JoinPartyLeaderComm_Success,
		PeerIDs: tssNodes,
	}

	// we put ourselves(leader) in the online list, so need threshold +1
	if len(onlinePeers) < peerGroup.threshold+1 {
		// we notify the failure of the join party to everyone
		msg.Type = messages.JoinPartyLeaderComm_Timeout
		pc.logger.Debug().Msgf("sending timeout response to %d all peers", len(onlinePeers))
		pc.sendResponseToAll(&msg, allPeers)
		return onlinePeers, ErrJoinPartyTimeout
	}
	// we notify all the peers who to run keygen/keysign
	// if a nodes is not in the list, it means he is not selected by the leader to run the tss
	pc.logger.Debug().Msgf("sending success response to %d all peers", len(allPeers))
	pc.sendResponseToAll(&msg, allPeers)
	return onlinePeers, nil
}

func (pc *PartyCoordinator) JoinPartyWithLeader(
	msgID string,
	blockHeight int64,
	peers []peer.ID,
	threshold int,
	sigChan chan string,
) ([]peer.ID, peer.ID, error) {
	leader, err := PickLeader(msgID, blockHeight, peers)
	if err != nil {
		return nil, "", errors.Wrap(err, "failed to pick leader")
	}

	peerGroup, err := pc.createJoinPartyGroups(msgID, leader, peers, threshold)
	if err != nil {
		return nil, leader, errors.Wrap(err, "error creating peerStatus")
	}

	defer pc.RemovePeerGroup(msgID)

	if pc.host.ID() == leader {
		onlinePeers, err := pc.joinPartyLeader(msgID, peerGroup, sigChan)

		return onlinePeers, leader, err
	}

	// now we are just the normal peer
	onlines, err := pc.joinPartyMember(msgID, peerGroup, sigChan)
	return onlines, leader, err
}

func (pc *PartyCoordinator) FreeStreams(msgID string) {
	pc.streamMgr.Free(msgID)
}
