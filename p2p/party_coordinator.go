package p2p

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	"github.com/libp2p/go-libp2p/core/host"
	"github.com/libp2p/go-libp2p/core/network"
	"github.com/libp2p/go-libp2p/core/peer"
	"github.com/libp2p/go-libp2p/core/protocol"
	"github.com/rs/zerolog"
	"google.golang.org/protobuf/proto"

	"github.com/zeta-chain/go-tss/conversion"
	"github.com/zeta-chain/go-tss/logs"
	"github.com/zeta-chain/go-tss/messages"
)

var (
	ErrJoinPartyTimeout = errors.New("fail to join party, timeout")
	ErrLeaderNotReady   = errors.New("leader not reachable")
	ErrSignReceived     = errors.New("signature received")
	ErrNotActiveSigner  = errors.New("not active signer")
	ErrSigGenerated     = errors.New("signature generated")
)

type PartyCoordinator struct {
	logger             zerolog.Logger
	host               host.Host
	stopChan           chan struct{}
	timeout            time.Duration
	peersGroup         map[string]*peerStatus
	joinPartyGroupLock *sync.Mutex
	streamMgr          *StreamMgr
}

// NewPartyCoordinator create a new instance of PartyCoordinator
func NewPartyCoordinator(host host.Host, timeout time.Duration, logger zerolog.Logger) *PartyCoordinator {
	logger = logger.With().Str(logs.Component, "party_coordinator").Logger()

	// if no timeout is given, default to 10 seconds
	if timeout.Nanoseconds() == 0 {
		timeout = 10 * time.Second
	}
	pc := &PartyCoordinator{
		logger:             logger,
		host:               host,
		stopChan:           make(chan struct{}),
		timeout:            timeout,
		peersGroup:         make(map[string]*peerStatus),
		joinPartyGroupLock: &sync.Mutex{},
		streamMgr:          NewStreamMgr(),
	}
	host.SetStreamHandler(joinPartyProtocolWithLeader, pc.HandleStreamWithLeader)
	return pc
}

// Stop the PartyCoordinator rune
func (pc *PartyCoordinator) Stop() {
	defer pc.logger.Info().Msg("stopping party coordinator")
	pc.host.RemoveStreamHandler(joinPartyProtocolWithLeader)
	close(pc.stopChan)
}

func (pc *PartyCoordinator) processRespMsg(respMsg *messages.JoinPartyLeaderComm, stream network.Stream) {
	remotePeer := stream.Conn().RemotePeer()

	pc.joinPartyGroupLock.Lock()
	peerGroup, ok := pc.peersGroup[respMsg.ID]
	pc.joinPartyGroupLock.Unlock()

	if !ok {
		pc.logger.Info().
			Stringer(logs.Peer, remotePeer).
			Msg("message ID from peer can not be found")
		_ = stream.Reset()
		return
	}

	pc.streamMgr.AddStream(respMsg.ID, stream)

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

	err := WriteStreamWithBuffer([]byte("done"), stream)
	if err != nil {
		pc.logger.Error().Err(err).
			Stringer(logs.Peer, remotePeer).
			Stringer(logs.Leader, leaderID).
			Str(logs.MsgID, respMsg.ID).
			Msg("Failed to write the reply to peer")
	}
}

func (pc *PartyCoordinator) processReqMsg(requestMsg *messages.JoinPartyLeaderComm, stream network.Stream) {
	pc.streamMgr.AddStream(requestMsg.ID, stream)

	pc.joinPartyGroupLock.Lock()
	peerGroup, ok := pc.peersGroup[requestMsg.ID]
	pc.joinPartyGroupLock.Unlock()

	if !ok {
		_ = stream.Reset()
		pc.logger.Info().Str(logs.MsgID, requestMsg.ID).Msg("This party is not ready")
		return
	}

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
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}

	var msg messages.JoinPartyLeaderComm
	err = proto.Unmarshal(payload, &msg)
	if err != nil {
		logger.Err(err).Msg("fail to unmarshal party data")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}

	logger.Debug().Msgf("received message type=%s", msg.MsgType)

	switch msg.MsgType {
	case "request":
		pc.processReqMsg(&msg, stream)
		return
	case "response":
		pc.processRespMsg(&msg, stream)
		err := WriteStreamWithBuffer([]byte("done"), stream)
		if err != nil {
			pc.logger.Error().Err(err).Msg("Failed to send response to leader")
		}
		return
	default:
		logger.Err(err).Msg("fail to process this message")
		pc.streamMgr.AddStream("UNKNOWN", stream)
		return
	}
}

func (pc *PartyCoordinator) RemovePeerGroup(messageID string) {
	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	delete(pc.peersGroup, messageID)
}

func (pc *PartyCoordinator) createJoinPartyGroups(
	messageID string,
	leaderID peer.ID,
	peerIDs []peer.ID,
	threshold int,
) (*peerStatus, error) {
	pc.joinPartyGroupLock.Lock()
	defer pc.joinPartyGroupLock.Unlock()
	peerStatus := newPeerStatus(peerIDs, pc.host.ID(), leaderID, threshold)
	pc.peersGroup[messageID] = peerStatus
	return peerStatus, nil
}

func (pc *PartyCoordinator) getPeerIDs(ids []string) ([]peer.ID, error) {
	result := make([]peer.ID, len(ids))
	for i, item := range ids {
		pid, err := peer.Decode(item)
		if err != nil {
			return nil, fmt.Errorf("fail to decode peer id(%s):%w", item, err)
		}
		result[i] = pid
	}
	return result, nil
}

func (pc *PartyCoordinator) sendResponseToAll(msg *messages.JoinPartyLeaderComm, peers []peer.ID) {
	msg.MsgType = "response"
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		pc.logger.Error().Err(err).Msg("error marshalling response")
		return
	}
	var wg sync.WaitGroup
	wg.Add(len(peers))
	for _, el := range peers {
		go func(peer peer.ID) {
			defer wg.Done()
			if peer == pc.host.ID() {
				return
			}
			if err := pc.sendMsgToPeer(msgSend, msg.ID, peer, joinPartyProtocolWithLeader, true); err != nil {
				pc.logger.Error().Err(err).Msg("error in send the join party request to peer")
			}
		}(el)
	}
	wg.Wait()
}

func (pc *PartyCoordinator) sendRequestToLeader(msg *messages.JoinPartyLeaderComm, leader peer.ID) error {
	msg.MsgType = "request"
	msgSend, err := proto.Marshal(msg)
	if err != nil {
		pc.logger.Error().Err(err).Msg("error marshalling request")
		return err
	}

	if err := pc.sendMsgToPeer(msgSend, msg.ID, leader, joinPartyProtocolWithLeader, false); err != nil {
		pc.logger.Error().Err(err).Msg("error in send the join party request to leader")
		return errors.New("fail to send request to leader")
	}

	return nil
}

func (pc *PartyCoordinator) sendMsgToPeer(
	msgBuf []byte,
	msgID string,
	remotePeer peer.ID,
	protoc protocol.ID,
	needResponse bool,
) error {
	ctx, cancel := context.WithTimeout(context.Background(), time.Second*4)
	defer cancel()

	pc.logger.Debug().Msgf("try to open stream to (%s) ", remotePeer)
	stream, err := pc.host.NewStream(ctx, remotePeer, protoc)
	if err != nil {
		streamError := fmt.Errorf("fail to create stream to peer(%s):%w", remotePeer, err)
		return streamError
	}

	defer func() {
		pc.streamMgr.AddStream(msgID, stream)
		if err := stream.Close(); err != nil {
			pc.logger.Error().Err(err).
				Str(logs.MsgID, msgID).
				Msg("Failed to close stream")
		}
	}()

	pc.logger.Debug().Msgf("open stream to (%s) successfully", remotePeer)
	err = WriteStreamWithBuffer(msgBuf, stream)
	if err != nil {
		return fmt.Errorf("fail to write message to stream:%w", err)
	}

	if needResponse {
		_, err := ReadStreamWithBuffer(stream)
		if err != nil {
			pc.logger.Error().Err(err).
				Stringer(logs.Peer, remotePeer).
				Msg("Failed to await the response from peer")
		}
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
		ID: msgID,
	}

	var wg sync.WaitGroup
	done := make(chan struct{})
	wg.Add(1)
	go func() {
		defer wg.Done()
		for {
			select {
			case <-done:
				return
			default:
				pc.logger.Trace().Msg("sending request message to leader")
				err := pc.sendRequestToLeader(&msg, leaderID)
				if err != nil {
					pc.logger.Error().Err(err).Msg("error sending request to leader")
				}
			}
			time.Sleep(time.Millisecond * 500)
		}
	}()

	var stopped bool
	var sigNotify string
	// now we wait for the leader to notify us who we do the keygen/keysign with
	select {
	case <-pc.stopChan:
		// promptly tear down this goroutine if partyCoordinator is stopped
		pc.logger.Debug().Msg("party coordinator stopped")
		stopped = true
	case <-peerGroup.notify:
		pc.logger.Debug().Msg("received a response from the leader")
	case <-time.After(pc.timeout):
		pc.logger.Debug().Msgf("timed out waiting for a response from the leader after %s", pc.timeout)
	case result := <-sigChan:
		pc.logger.Debug().Msgf("received %s from sigChan", result)
		sigNotify = result
	}

	close(done)
	wg.Wait()

	if sigNotify == "signature received" {
		return nil, ErrSignReceived
	}

	if peerGroup.getLeaderResponse() == nil {
		leaderPk, err := conversion.GetPubKeyFromPeerID(leaderID.String())
		if err != nil {
			pc.logger.Error().Msg("received no response from the leader")
		} else {
			pc.logger.Error().Str(logs.Leader, leaderPk).Msg("received no response from the leader")
		}
		return nil, ErrLeaderNotReady
	}

	onlineNodes := peerGroup.getLeaderResponse().PeerIDs
	// we trust the returned nodes returned by the leader, if tss fail, the leader
	// also will get blamed.
	pIDs, err := pc.getPeerIDs(onlineNodes)
	if err != nil {
		pc.logger.Error().Err(err).Msg("fail to parse peer ids")
		return nil, err
	}

	if len(pIDs) < peerGroup.threshold {
		return pIDs, errors.New("not enough peers")
	}

	pc.logger.Trace().Msgf("leader response message type=%s", peerGroup.getLeaderResponse().Type.String())
	if peerGroup.getLeaderResponse().Type == messages.JoinPartyLeaderComm_Success {
		return pIDs, nil
	}

	if stopped {
		pc.logger.Trace().Msg("join party stopped")
	} else {
		pc.logger.Trace().Msg("join party timedout")
	}
	return pIDs, ErrJoinPartyTimeout
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
		pc.logger.Debug().Msgf("leader timedout waiting for peers after %s", pc.timeout/2)
	case result := <-sigChan:
		sigNotify = result
	}
	if sigNotify == "signature received" {
		return nil, ErrSignReceived
	}
	allPeers := peerGroup.getAllPeers()
	onlinePeers, _ := peerGroup.getPeersStatus()
	onlinePeers = append(onlinePeers, pc.host.ID())

	tssNodes := make([]string, len(onlinePeers))
	for i, el := range onlinePeers {
		tssNodes[i] = el.String()
	}

	msg := messages.JoinPartyLeaderComm{
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
	peers []string,
	threshold int,
	sigChan chan string,
) ([]peer.ID, string, error) {
	leader, err := LeaderNode(msgID, blockHeight, peers)
	if err != nil {
		return nil, "", err
	}

	leaderID, err := peer.Decode(leader)
	if err != nil {
		return nil, "", err
	}

	peerIDs, err := pc.getPeerIDs(peers)
	if err != nil {
		return nil, "", err
	}

	peerGroup, err := pc.createJoinPartyGroups(msgID, leaderID, peerIDs, threshold)
	if err != nil {
		pc.logger.Error().Err(err).Msg("error creating peerStatus")
		return nil, leader, err
	}

	defer pc.RemovePeerGroup(msgID)

	if pc.host.ID() == leaderID {
		onlines, err := pc.joinPartyLeader(msgID, peerGroup, sigChan)
		return onlines, leader, err
	}
	// now we are just the normal peer
	onlines, err := pc.joinPartyMember(msgID, peerGroup, sigChan)
	return onlines, leader, err
}

func (pc *PartyCoordinator) ReleaseStream(msgID string) {
	pc.streamMgr.ReleaseStream(msgID)
}
