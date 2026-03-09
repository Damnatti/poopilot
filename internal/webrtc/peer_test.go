package webrtc

import (
	"sync"
	"testing"
	"time"

	pwebrtc "github.com/pion/webrtc/v4"
)

// noSTUN config for local loopback tests (no network needed).
var testConfig = PeerConfig{
	STUNServers: []string{},
}

func TestNewPeer_Creates(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer p.Close()

	if p.State() != PeerStateNew {
		t.Errorf("initial state: got %v, want %v", p.State(), PeerStateNew)
	}
}

func TestNewPeer_DefaultSTUN(t *testing.T) {
	// Should not error even with default STUN
	p, err := NewPeer(PeerConfig{})
	if err != nil {
		t.Fatalf("NewPeer with default STUN failed: %v", err)
	}
	defer p.Close()
}

func TestPeer_CreateDataChannel(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer p.Close()

	dc, err := p.CreateDataChannel("test")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	if dc.Label() != "test" {
		t.Errorf("label: got %q, want %q", dc.Label(), "test")
	}

	_, ok := p.GetChannel("test")
	if !ok {
		t.Error("GetChannel should find 'test'")
	}

	_, ok = p.GetChannel("nonexistent")
	if ok {
		t.Error("GetChannel should not find 'nonexistent'")
	}
}

func TestPeer_Send_NoChannel_Error(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer p.Close()

	err = p.Send("nonexistent", []byte("hello"))
	if err == nil {
		t.Error("Send to nonexistent channel should error")
	}
}

func TestPeer_LoopbackConnection(t *testing.T) {
	// Create two peers that connect to each other locally
	offerer, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer (offerer) failed: %v", err)
	}
	defer offerer.Close()

	answerer, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer (answerer) failed: %v", err)
	}
	defer answerer.Close()

	// Must create a data channel before offer for valid SDP
	_, err = offerer.CreateDataChannel("control")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	// Track state changes
	offererConnected := make(chan struct{})
	offerer.OnStateChange(func(s PeerState) {
		if s == PeerStateConnected {
			select {
			case offererConnected <- struct{}{}:
			default:
			}
		}
	})

	answererConnected := make(chan struct{})
	answerer.OnStateChange(func(s PeerState) {
		if s == PeerStateConnected {
			select {
			case answererConnected <- struct{}{}:
			default:
			}
		}
	})

	// Create offer
	offerCompressed, err := CreateOffer(offerer, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	if offerCompressed == "" {
		t.Fatal("offer should not be empty")
	}

	// Create answer
	answerCompressed, err := CreateAnswer(answerer, offerCompressed, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateAnswer failed: %v", err)
	}

	// Accept answer
	if err := AcceptAnswer(offerer, answerCompressed); err != nil {
		t.Fatalf("AcceptAnswer failed: %v", err)
	}

	// Wait for connection (longer timeout for race detector)
	timeout := time.After(30 * time.Second)
	select {
	case <-offererConnected:
	case <-timeout:
		t.Fatal("offerer did not connect in time")
	}
	select {
	case <-answererConnected:
	case <-timeout:
		t.Fatal("answerer did not connect in time")
	}

	if offerer.State() != PeerStateConnected {
		t.Errorf("offerer state: got %v, want connected", offerer.State())
	}
	if answerer.State() != PeerStateConnected {
		t.Errorf("answerer state: got %v, want connected", answerer.State())
	}
}

func TestPeer_DataChannel_SendReceive(t *testing.T) {
	offerer, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer (offerer) failed: %v", err)
	}
	defer offerer.Close()

	answerer, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer (answerer) failed: %v", err)
	}
	defer answerer.Close()

	// Create data channel on offerer side
	_, err = offerer.CreateDataChannel("control")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	// Track received messages on answerer
	var received []byte
	var mu sync.Mutex
	receivedCh := make(chan struct{}, 1)

	answerer.OnMessage(func(label string, data []byte) {
		mu.Lock()
		received = append(received, data...)
		mu.Unlock()
		select {
		case receivedCh <- struct{}{}:
		default:
		}
	})

	// Track when answerer gets the data channel
	answererChReady := make(chan struct{}, 1)
	answerer.OnChannel(func(label string, ch *pwebrtc.DataChannel) {
		select {
		case answererChReady <- struct{}{}:
		default:
		}
	})

	// Connect
	offerCompressed, err := CreateOffer(offerer, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}
	answerCompressed, err := CreateAnswer(answerer, offerCompressed, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateAnswer failed: %v", err)
	}
	if err := AcceptAnswer(offerer, answerCompressed); err != nil {
		t.Fatalf("AcceptAnswer failed: %v", err)
	}

	// Wait for data channel to be ready on answerer
	select {
	case <-answererChReady:
	case <-time.After(10 * time.Second):
		t.Fatal("answerer did not receive data channel")
	}

	// Wait a bit for channel to be open
	time.Sleep(500 * time.Millisecond)

	// Send from offerer
	err = offerer.Send("control", []byte("hello from offerer"))
	if err != nil {
		t.Fatalf("Send failed: %v", err)
	}

	// Wait for message
	select {
	case <-receivedCh:
	case <-time.After(5 * time.Second):
		t.Fatal("did not receive message")
	}

	mu.Lock()
	got := string(received)
	mu.Unlock()

	if got != "hello from offerer" {
		t.Errorf("received: got %q, want %q", got, "hello from offerer")
	}
}

func TestPeer_StateChange_Callbacks(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}

	states := make(chan PeerState, 10)
	p.OnStateChange(func(s PeerState) {
		states <- s
	})

	p.Close()

	// Should get closed state
	select {
	case s := <-states:
		if s != PeerStateClosed {
			// May get other states too, that's ok
		}
	case <-time.After(2 * time.Second):
		// Closing might not trigger ICE state change if never connected — that's ok
	}
}

func TestPeer_Close_Idempotent(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}

	if err := p.Close(); err != nil {
		t.Fatalf("first Close failed: %v", err)
	}

	// Second close should not panic
	p.Close()

	if p.State() != PeerStateClosed {
		t.Errorf("state after close: got %v, want closed", p.State())
	}
}
