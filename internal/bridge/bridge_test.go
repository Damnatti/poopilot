package bridge

import (
	"context"
	"encoding/json"
	"strings"
	"sync"
	"testing"
	"time"

	pwebrtc "github.com/pion/webrtc/v4"

	"github.com/Damnatti/poopilot/internal/approval"
	"github.com/Damnatti/poopilot/internal/protocol"
	"github.com/Damnatti/poopilot/internal/pty"
	rtc "github.com/Damnatti/poopilot/internal/webrtc"
)

// setupLoopback creates two connected peers and returns (offerer, answerer).
// The answerer will have the "control" data channel ready for sending.
func setupLoopback(t *testing.T) (*rtc.Peer, *rtc.Peer) {
	t.Helper()

	config := rtc.PeerConfig{STUNServers: []string{}}

	offerer, err := rtc.NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer (offerer): %v", err)
	}

	answerer, err := rtc.NewPeer(config)
	if err != nil {
		t.Fatalf("NewPeer (answerer): %v", err)
	}

	// Wait for answerer to receive the data channel
	answererChReady := make(chan struct{}, 1)
	answerer.OnChannel(func(label string, ch *pwebrtc.DataChannel) {
		select {
		case answererChReady <- struct{}{}:
		default:
		}
	})

	// Create control channel on offerer
	_, err = offerer.CreateDataChannel("control")
	if err != nil {
		t.Fatalf("CreateDataChannel: %v", err)
	}

	// Connect
	offer, err := rtc.CreateOffer(offerer, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateOffer: %v", err)
	}
	answer, err := rtc.CreateAnswer(answerer, offer, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateAnswer: %v", err)
	}
	if err := rtc.AcceptAnswer(offerer, answer); err != nil {
		t.Fatalf("AcceptAnswer: %v", err)
	}

	// Wait for connection
	connected := make(chan struct{})
	offerer.OnStateChange(func(s rtc.PeerState) {
		if s == rtc.PeerStateConnected {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-connected:
	case <-time.After(10 * time.Second):
		t.Fatal("peers did not connect")
	}

	// Wait for answerer to have the data channel
	select {
	case <-answererChReady:
	case <-time.After(5 * time.Second):
		t.Fatal("answerer did not receive data channel")
	}

	// Extra wait for channel to be fully open
	time.Sleep(500 * time.Millisecond)

	return offerer, answerer
}

// collectMessages collects messages received on a peer.
func collectMessages(p *rtc.Peer) (func() []protocol.Message, func()) {
	var msgs []protocol.Message
	var mu sync.Mutex
	ch := make(chan struct{}, 100)

	p.OnMessage(func(label string, data []byte) {
		msg, err := protocol.Decode(data)
		if err != nil {
			return
		}
		mu.Lock()
		msgs = append(msgs, msg)
		mu.Unlock()
		select {
		case ch <- struct{}{}:
		default:
		}
	})

	get := func() []protocol.Message {
		mu.Lock()
		defer mu.Unlock()
		result := make([]protocol.Message, len(msgs))
		copy(result, msgs)
		return result
	}

	wait := func() {
		select {
		case <-ch:
		case <-time.After(3 * time.Second):
		}
	}

	return get, wait
}

func TestBridge_TermOutput_ForwardsToChannel(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	// Collect messages on answerer (phone side)
	getMessages, waitMsg := collectMessages(answerer)

	// Create a session that produces output
	sess, err := mgr.Create("echo", []string{"bridge test output"})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	b.AttachSession(sess.ID)

	// Wait for output
	time.Sleep(1 * time.Second)
	waitMsg()

	msgs := getMessages()
	var found bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgTermOutput {
			if strings.Contains(string(msg.Payload), "bridge test output") {
				found = true
				break
			}
		}
	}

	if !found {
		t.Errorf("expected to receive terminal output with 'bridge test output', got %d messages", len(msgs))
	}
}

func TestBridge_TermInput_ForwardsToPTY(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	// Create cat session (echoes stdin)
	sess, err := mgr.Create("cat", nil)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	b.AttachSession(sess.ID)

	// Collect output messages
	getMessages, _ := collectMessages(answerer)

	// Send input from phone (answerer) to bridge (offerer)
	inputMsg := protocol.Message{
		Type:      protocol.MsgTermInput,
		SessionID: sess.ID,
		Payload:   []byte("hello from phone\n"),
	}
	encoded, _ := protocol.Encode(inputMsg)
	answerer.Send("control", encoded)

	// Wait for echo
	time.Sleep(1 * time.Second)

	msgs := getMessages()
	var found bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgTermOutput && strings.Contains(string(msg.Payload), "hello from phone") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected echoed input in terminal output")
	}
}

func TestBridge_ApprovalDetected_SendsRequest(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	// Collect messages on phone side
	getMessages, _ := collectMessages(answerer)

	// Create a session that outputs an approval prompt
	sess, err := mgr.Create("echo", []string{"Do you want to proceed? [Y/n]"})
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	b.AttachSession(sess.ID)

	// Wait for detection
	time.Sleep(1 * time.Second)

	msgs := getMessages()
	var found bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgApprovalReq {
			var req protocol.ApprovalRequest
			if err := protocol.UnmarshalPayload(msg.Payload, &req); err == nil {
				if req.SessionID == sess.ID {
					found = true
					break
				}
			}
		}
	}

	if !found {
		types := make([]protocol.MsgType, len(msgs))
		for i, m := range msgs {
			types[i] = m.Type
		}
		t.Errorf("expected MsgApprovalReq for session %s, got message types: %v", sess.ID, types)
	}
}

func TestBridge_ApprovalApproved_WritesYes(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	// Create cat session
	sess, err := mgr.Create("cat", nil)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	b.AttachSession(sess.ID)

	// Simulate an approval request by adding it directly
	reqID := "testreq123456789"
	b.approvalMu.Lock()
	b.pendingApprovals[reqID] = pendingApproval{
		SessionID: sess.ID,
		Prompt:    "Proceed?",
	}
	b.approvalMu.Unlock()

	// Collect output
	getMessages, _ := collectMessages(answerer)

	// Send approval response from phone
	resp := protocol.ApprovalResponse{ID: reqID, Approved: true}
	payload, _ := protocol.MarshalPayload(resp)
	approvalMsg := protocol.Message{
		Type:    protocol.MsgApprovalResp,
		Payload: payload,
	}
	encoded, _ := protocol.Encode(approvalMsg)
	answerer.Send("control", encoded)

	// Wait for "y\n" to be echoed by cat
	time.Sleep(1 * time.Second)

	msgs := getMessages()
	var found bool
	for _, msg := range msgs {
		if msg.Type == protocol.MsgTermOutput && strings.Contains(string(msg.Payload), "y") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected 'y' in terminal output after approval")
	}
}

func TestBridge_ApprovalDenied_WritesNo(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	sess, err := mgr.Create("cat", nil)
	if err != nil {
		t.Fatalf("Create session: %v", err)
	}
	b.AttachSession(sess.ID)

	reqID := "denyreq123456789"
	b.approvalMu.Lock()
	b.pendingApprovals[reqID] = pendingApproval{
		SessionID: sess.ID,
		Prompt:    "Proceed?",
	}
	b.approvalMu.Unlock()

	getMessages, _ := collectMessages(answerer)

	resp := protocol.ApprovalResponse{ID: reqID, Approved: false}
	payload, _ := protocol.MarshalPayload(resp)
	msg := protocol.Message{
		Type:    protocol.MsgApprovalResp,
		Payload: payload,
	}
	encoded, _ := protocol.Encode(msg)
	answerer.Send("control", encoded)

	time.Sleep(1 * time.Second)

	msgs := getMessages()
	var found bool
	for _, m := range msgs {
		if m.Type == protocol.MsgTermOutput && strings.Contains(string(m.Payload), "n") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected 'n' in terminal output after denial")
	}
}

func TestBridge_SessionCreate_FromPhone(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	getMessages, _ := collectMessages(answerer)

	// Send session create from phone
	req := protocol.SessionCreateReq{Command: "echo", Args: []string{"created from phone"}}
	payload, _ := protocol.MarshalPayload(req)
	msg := protocol.Message{
		Type:    protocol.MsgSessionCreate,
		Payload: payload,
	}
	encoded, _ := protocol.Encode(msg)
	answerer.Send("control", encoded)

	// Wait for session list update
	time.Sleep(1 * time.Second)

	msgs := getMessages()
	var foundList bool
	for _, m := range msgs {
		if m.Type == protocol.MsgSessionList {
			var infos []protocol.SessionInfo
			if err := json.Unmarshal(m.Payload, &infos); err == nil && len(infos) > 0 {
				foundList = true
				break
			}
		}
	}

	if !foundList {
		t.Error("expected MsgSessionList after session create")
	}

	if mgr.Count() < 1 {
		t.Error("expected at least 1 session in manager")
	}
}

func TestBridge_Ping_ReturnsPong(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	getMessages, waitMsg := collectMessages(answerer)

	// Send ping from phone
	pingMsg := protocol.Message{Type: protocol.MsgPing}
	encoded, _ := protocol.Encode(pingMsg)
	answerer.Send("control", encoded)

	waitMsg()
	time.Sleep(500 * time.Millisecond)

	msgs := getMessages()
	var foundPong bool
	for _, m := range msgs {
		if m.Type == protocol.MsgPong {
			foundPong = true
			break
		}
	}

	if !foundPong {
		types := make([]protocol.MsgType, len(msgs))
		for i, m := range msgs {
			types[i] = m.Type
		}
		t.Errorf("expected MsgPong, got types: %v", types)
	}
}

func TestBridge_Scrollback_SendsBuffer(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	// Create session that produces output
	sess, err := mgr.Create("echo", []string{"scrollback data"})
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b.AttachSession(sess.ID)

	// Wait for echo to complete and buffer to fill
	select {
	case <-sess.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("timeout waiting for session to exit")
	}
	time.Sleep(200 * time.Millisecond)

	// Now collect and send scrollback
	getMessages, _ := collectMessages(answerer)

	err = b.SendScrollback(sess.ID)
	if err != nil {
		t.Fatalf("SendScrollback: %v", err)
	}

	time.Sleep(500 * time.Millisecond)

	msgs := getMessages()
	var found bool
	for _, m := range msgs {
		if m.Type == protocol.MsgScrollback && strings.Contains(string(m.Payload), "scrollback data") {
			found = true
			break
		}
	}

	if !found {
		t.Error("expected scrollback with 'scrollback data'")
	}
}

func TestBridge_DetachSession_StopsStreaming(t *testing.T) {
	offerer, answerer := setupLoopback(t)
	defer offerer.Close()
	defer answerer.Close()

	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	detector := approval.NewDetector()
	b := New(mgr, offerer, detector)
	b.Start(context.Background())

	sess, err := mgr.Create("cat", nil)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	b.AttachSession(sess.ID)

	// Detach
	b.DetachSession(sess.ID)

	b.mu.Lock()
	_, exists := b.streams[sess.ID]
	b.mu.Unlock()

	if exists {
		t.Error("stream should be removed after DetachSession")
	}
}
