package webrtc

import (
	"fmt"
	"sync"
	"time"

	"github.com/pion/webrtc/v4"
)

// PeerState represents the connection state of a peer.
type PeerState int

const (
	PeerStateNew PeerState = iota
	PeerStateConnecting
	PeerStateConnected
	PeerStateDisconnected
	PeerStateFailed
	PeerStateClosed
)

func (s PeerState) String() string {
	switch s {
	case PeerStateNew:
		return "new"
	case PeerStateConnecting:
		return "connecting"
	case PeerStateConnected:
		return "connected"
	case PeerStateDisconnected:
		return "disconnected"
	case PeerStateFailed:
		return "failed"
	case PeerStateClosed:
		return "closed"
	default:
		return "unknown"
	}
}

// DefaultSTUNServers are free Google STUN servers.
var DefaultSTUNServers = []string{
	"stun:stun.l.google.com:19302",
	"stun:stun1.l.google.com:19302",
}

// PeerConfig configures the WebRTC peer connection.
type PeerConfig struct {
	STUNServers []string
}

// Peer wraps a WebRTC PeerConnection with data channel management.
type Peer struct {
	pc    *webrtc.PeerConnection
	state PeerState
	mu    sync.RWMutex

	channels map[string]*webrtc.DataChannel

	onMessage     func(label string, data []byte)
	onStateChange func(PeerState)
	onChannel     func(label string, ch *webrtc.DataChannel)
}

// NewPeer creates a new WebRTC peer connection.
func NewPeer(config PeerConfig) (*Peer, error) {
	stunServers := config.STUNServers
	if len(stunServers) == 0 {
		stunServers = DefaultSTUNServers
	}

	iceServers := []webrtc.ICEServer{
		{URLs: stunServers},
	}

	pc, err := webrtc.NewPeerConnection(webrtc.Configuration{
		ICEServers: iceServers,
	})
	if err != nil {
		return nil, fmt.Errorf("new peer connection: %w", err)
	}

	p := &Peer{
		pc:       pc,
		state:    PeerStateNew,
		channels: make(map[string]*webrtc.DataChannel),
	}

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		var ps PeerState
		switch state {
		case webrtc.ICEConnectionStateConnected:
			ps = PeerStateConnected
		case webrtc.ICEConnectionStateDisconnected:
			ps = PeerStateDisconnected
		case webrtc.ICEConnectionStateFailed:
			ps = PeerStateFailed
		case webrtc.ICEConnectionStateClosed:
			ps = PeerStateClosed
		case webrtc.ICEConnectionStateChecking:
			ps = PeerStateConnecting
		default:
			return
		}

		p.mu.Lock()
		p.state = ps
		cb := p.onStateChange
		p.mu.Unlock()

		if cb != nil {
			cb(ps)
		}
	})

	pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		label := dc.Label()

		p.mu.Lock()
		p.channels[label] = dc
		chCb := p.onChannel
		p.mu.Unlock()

		p.setupChannelHandlers(dc)

		if chCb != nil {
			chCb(label, dc)
		}
	})

	return p, nil
}

// CreateDataChannel creates a new outgoing data channel.
func (p *Peer) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	dc, err := p.pc.CreateDataChannel(label, nil)
	if err != nil {
		return nil, fmt.Errorf("create data channel %q: %w", label, err)
	}

	p.mu.Lock()
	p.channels[label] = dc
	p.mu.Unlock()

	p.setupChannelHandlers(dc)

	return dc, nil
}

// Send sends data on the named data channel.
func (p *Peer) Send(label string, data []byte) error {
	p.mu.RLock()
	dc, ok := p.channels[label]
	p.mu.RUnlock()

	if !ok {
		return fmt.Errorf("data channel %q not found", label)
	}

	return dc.Send(data)
}

// GetChannel returns a data channel by label.
func (p *Peer) GetChannel(label string) (*webrtc.DataChannel, bool) {
	p.mu.RLock()
	defer p.mu.RUnlock()
	dc, ok := p.channels[label]
	return dc, ok
}

// OnMessage sets the callback for incoming messages on any data channel.
func (p *Peer) OnMessage(f func(label string, data []byte)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onMessage = f
}

// OnStateChange sets the callback for connection state changes.
func (p *Peer) OnStateChange(f func(PeerState)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onStateChange = f
}

// OnChannel sets the callback for incoming data channels.
func (p *Peer) OnChannel(f func(label string, ch *webrtc.DataChannel)) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.onChannel = f
}

// State returns the current peer state.
func (p *Peer) State() PeerState {
	p.mu.RLock()
	defer p.mu.RUnlock()
	return p.state
}

// PeerConnection returns the underlying pion PeerConnection.
func (p *Peer) PeerConnection() *webrtc.PeerConnection {
	return p.pc
}

// WaitDisconnect blocks until the peer disconnects, fails, or closes.
// Uses polling to avoid interfering with OnStateChange callbacks.
func (p *Peer) WaitDisconnect() {
	for {
		p.mu.RLock()
		s := p.state
		p.mu.RUnlock()
		if s == PeerStateDisconnected || s == PeerStateFailed || s == PeerStateClosed {
			return
		}
		time.Sleep(500 * time.Millisecond)
	}
}

// Close closes the peer connection and all channels.
func (p *Peer) Close() error {
	p.mu.Lock()
	p.state = PeerStateClosed
	p.mu.Unlock()
	return p.pc.Close()
}

func (p *Peer) setupChannelHandlers(dc *webrtc.DataChannel) {
	label := dc.Label()

	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		p.mu.RLock()
		cb := p.onMessage
		p.mu.RUnlock()

		if cb != nil {
			cb(label, msg.Data)
		}
	})
}
