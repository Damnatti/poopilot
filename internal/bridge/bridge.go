package bridge

import (
	"context"
	"encoding/json"
	"fmt"
	"sync"
	"time"

	"github.com/Damnatti/poopilot/internal/approval"
	"github.com/Damnatti/poopilot/internal/protocol"
	"github.com/Damnatti/poopilot/internal/pty"
	rtc "github.com/Damnatti/poopilot/internal/webrtc"
	"github.com/google/uuid"
)

// Bridge connects PTY sessions to a WebRTC peer, routing terminal I/O
// and handling approval prompts.
type Bridge struct {
	ptyMgr   *pty.Manager
	peer     *rtc.Peer
	detector *approval.Detector

	// per-session cancel funcs for output streaming goroutines
	streams map[string]context.CancelFunc
	mu      sync.Mutex

	// pending approval requests awaiting response
	pendingApprovals map[string]pendingApproval
	approvalMu       sync.Mutex
}

type pendingApproval struct {
	SessionID string
	Prompt    string
}

// New creates a new Bridge.
func New(ptyMgr *pty.Manager, peer *rtc.Peer, detector *approval.Detector) *Bridge {
	return &Bridge{
		ptyMgr:           ptyMgr,
		peer:             peer,
		detector:         detector,
		streams:          make(map[string]context.CancelFunc),
		pendingApprovals: make(map[string]pendingApproval),
	}
}

// Start begins listening for messages from the peer and sets up routing.
func (b *Bridge) Start(ctx context.Context) {
	b.peer.OnMessage(func(label string, data []byte) {
		b.handleMessage(label, data)
	})

	// When peer connects, send session info with retries
	// (data channel may not be open immediately after ICE connects)
	b.peer.OnStateChange(func(s rtc.PeerState) {
		if s == rtc.PeerStateConnected {
			go func() {
				for i := 0; i < 10; i++ {
					time.Sleep(500 * time.Millisecond)
					if err := b.sendSessionList(); err == nil {
						b.SendAllScrollback()
						return
					}
				}
			}()
		}
	})
}

// AttachSession starts streaming a PTY session's output over WebRTC.
func (b *Bridge) AttachSession(sessionID string) error {
	sess, ok := b.ptyMgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	b.mu.Lock()
	if _, exists := b.streams[sessionID]; exists {
		b.mu.Unlock()
		return nil // already attached
	}
	b.mu.Unlock()

	ctx, cancel := context.WithCancel(context.Background())

	b.mu.Lock()
	b.streams[sessionID] = cancel
	b.mu.Unlock()

	sess.OnOutput(func(data []byte) {
		select {
		case <-ctx.Done():
			return
		default:
		}

		// Check for approval prompts
		detections := b.detector.Scan(data)
		for _, det := range detections {
			b.sendApprovalRequest(sessionID, det)
		}

		// Forward output to phone
		msg := protocol.Message{
			Type:      protocol.MsgTermOutput,
			SessionID: sessionID,
			Payload:   data,
		}
		encoded, err := protocol.Encode(msg)
		if err != nil {
			return
		}
		b.peer.Send("control", encoded)
	})

	sess.OnExit(func(code int) {
		b.sendSessionList()
	})

	return nil
}

// DetachSession stops streaming a session's output.
func (b *Bridge) DetachSession(sessionID string) {
	b.mu.Lock()
	cancel, ok := b.streams[sessionID]
	if ok {
		cancel()
		delete(b.streams, sessionID)
	}
	b.mu.Unlock()
}

// SendScrollback sends the buffered output of a session to the phone.
func (b *Bridge) SendScrollback(sessionID string) error {
	sess, ok := b.ptyMgr.Get(sessionID)
	if !ok {
		return fmt.Errorf("session %s not found", sessionID)
	}

	snap := sess.ScrollbackSnapshot()
	if len(snap) == 0 {
		return nil
	}

	msg := protocol.Message{
		Type:      protocol.MsgScrollback,
		SessionID: sessionID,
		Payload:   snap,
	}
	encoded, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return b.peer.Send("control", encoded)
}

// SendAllScrollback sends scrollback for all active sessions.
func (b *Bridge) SendAllScrollback() {
	for _, info := range b.ptyMgr.List() {
		b.SendScrollback(info.ID)
	}
}

func (b *Bridge) handleMessage(label string, data []byte) {
	msg, err := protocol.Decode(data)
	if err != nil {
		return
	}

	switch msg.Type {
	case protocol.MsgTermInput:
		b.handleTermInput(msg)
	case protocol.MsgTermResize:
		b.handleTermResize(msg)
	case protocol.MsgApprovalResp:
		b.handleApprovalResponse(msg)
	case protocol.MsgSessionCreate:
		b.handleSessionCreate(msg)
	case protocol.MsgSessionClose:
		b.handleSessionClose(msg)
	case protocol.MsgSessionSwitch:
		b.handleSessionSwitch(msg)
	case protocol.MsgPing:
		b.handlePing()
	}
}

func (b *Bridge) handleTermInput(msg protocol.Message) {
	sess, ok := b.ptyMgr.Get(msg.SessionID)
	if !ok {
		return
	}
	data := msg.Payload
	for len(data) > 0 {
		n, err := sess.Write(data)
		if err != nil {
			return
		}
		data = data[n:]
	}
}

func (b *Bridge) handleTermResize(msg protocol.Message) {
	var resize protocol.TermResize
	if err := protocol.UnmarshalPayload(msg.Payload, &resize); err != nil {
		return
	}

	sess, ok := b.ptyMgr.Get(msg.SessionID)
	if !ok {
		return
	}
	sess.Resize(resize.Rows, resize.Cols)
}

func (b *Bridge) handleApprovalResponse(msg protocol.Message) {
	var resp protocol.ApprovalResponse
	if err := protocol.UnmarshalPayload(msg.Payload, &resp); err != nil {
		return
	}

	b.approvalMu.Lock()
	pending, ok := b.pendingApprovals[resp.ID]
	if ok {
		delete(b.pendingApprovals, resp.ID)
	}
	b.approvalMu.Unlock()

	if !ok {
		return
	}

	sess, ok := b.ptyMgr.Get(pending.SessionID)
	if !ok {
		return
	}

	if resp.Approved {
		_, _ = sess.Write([]byte("y\n"))
	} else {
		_, _ = sess.Write([]byte("n\n"))
	}
}

func (b *Bridge) handleSessionCreate(msg protocol.Message) {
	var req protocol.SessionCreateReq
	if err := protocol.UnmarshalPayload(msg.Payload, &req); err != nil {
		b.sendError("INVALID_PAYLOAD", "invalid session create request")
		return
	}

	sess, err := b.ptyMgr.Create(req.Command, req.Args)
	if err != nil {
		b.sendError("SESSION_CREATE_FAILED", err.Error())
		return
	}

	b.AttachSession(sess.ID)
	b.sendSessionList()
}

func (b *Bridge) handleSessionClose(msg protocol.Message) {
	// SessionID is in payload as JSON for control messages
	var info struct {
		ID string `json:"id"`
	}
	if err := protocol.UnmarshalPayload(msg.Payload, &info); err != nil {
		return
	}

	b.DetachSession(info.ID)
	b.ptyMgr.Close(info.ID)
	b.sendSessionList()
}

func (b *Bridge) handleSessionSwitch(msg protocol.Message) {
	var info struct {
		ID string `json:"id"`
	}
	if err := protocol.UnmarshalPayload(msg.Payload, &info); err != nil {
		return
	}

	// Send scrollback for the switched-to session
	b.SendScrollback(info.ID)
}

func (b *Bridge) handlePing() {
	msg := protocol.Message{
		Type: protocol.MsgPong,
	}
	encoded, err := protocol.Encode(msg)
	if err != nil {
		return
	}
	b.peer.Send("control", encoded)
}

func (b *Bridge) sendApprovalRequest(sessionID string, det approval.Detection) {
	reqID := uuid.New().String()[:16]

	b.approvalMu.Lock()
	b.pendingApprovals[reqID] = pendingApproval{
		SessionID: sessionID,
		Prompt:    det.Prompt,
	}
	b.approvalMu.Unlock()

	req := protocol.ApprovalRequest{
		ID:        reqID,
		SessionID: sessionID,
		Prompt:    det.Prompt,
		Context:   det.Context,
		Timestamp: time.Now().Unix(),
	}

	payload, err := protocol.MarshalPayload(req)
	if err != nil {
		return
	}

	msg := protocol.Message{
		Type:    protocol.MsgApprovalReq,
		Payload: payload,
	}
	encoded, err := protocol.Encode(msg)
	if err != nil {
		return
	}
	b.peer.Send("control", encoded)
}

func (b *Bridge) sendSessionList() error {
	infos := b.ptyMgr.List()
	payload, err := json.Marshal(infos)
	if err != nil {
		return err
	}

	msg := protocol.Message{
		Type:    protocol.MsgSessionList,
		Payload: payload,
	}
	encoded, err := protocol.Encode(msg)
	if err != nil {
		return err
	}
	return b.peer.Send("control", encoded)
}

func (b *Bridge) sendError(code, message string) {
	errPayload := protocol.ErrorPayload{
		Code:    code,
		Message: message,
	}
	payload, err := protocol.MarshalPayload(errPayload)
	if err != nil {
		return
	}
	msg := protocol.Message{
		Type:    protocol.MsgError,
		Payload: payload,
	}
	encoded, err := protocol.Encode(msg)
	if err != nil {
		return
	}
	b.peer.Send("control", encoded)
}
