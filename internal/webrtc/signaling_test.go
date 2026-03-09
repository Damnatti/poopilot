package webrtc

import (
	"testing"
	"time"
)

func TestCompressDecompress_RoundTrip(t *testing.T) {
	payload := SignalingPayload{
		SDP:         "v=0\r\no=- 1234 1234 IN IP4 127.0.0.1\r\ns=-\r\n",
		STUNServers: []string{"stun:stun.l.google.com:19302"},
	}

	compressed, err := CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload failed: %v", err)
	}

	if compressed == "" {
		t.Fatal("compressed should not be empty")
	}

	decompressed, err := DecompressPayload(compressed)
	if err != nil {
		t.Fatalf("DecompressPayload failed: %v", err)
	}

	if decompressed.SDP != payload.SDP {
		t.Errorf("SDP mismatch: got %q, want %q", decompressed.SDP, payload.SDP)
	}
	if len(decompressed.STUNServers) != 1 || decompressed.STUNServers[0] != "stun:stun.l.google.com:19302" {
		t.Errorf("STUNServers mismatch: got %v", decompressed.STUNServers)
	}
}

func TestCompressPayload_IsSmall(t *testing.T) {
	// Simulate a realistic data-channel-only SDP (~400 bytes)
	sdp := `v=0
o=- 1234567890 1234567890 IN IP4 0.0.0.0
s=-
t=0 0
a=group:BUNDLE 0
a=extmap-allow-mixed
a=msid-semantic: WMS
m=application 9 UDP/DTLS/SCTP webrtc-datachannel
c=IN IP4 0.0.0.0
a=ice-ufrag:abcd
a=ice-pwd:abcdefghijklmnopqrstuvwx
a=fingerprint:sha-256 AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99:AA:BB:CC:DD:EE:FF:00:11:22:33:44:55:66:77:88:99
a=setup:actpass
a=mid:0
a=sctp-port:5000
a=candidate:1 1 UDP 2130706431 192.168.1.100 50000 typ host
a=candidate:2 1 UDP 1694498815 203.0.113.1 50001 typ srflx raddr 192.168.1.100 rport 50000
a=end-of-candidates
`

	payload := SignalingPayload{SDP: sdp}
	compressed, err := CompressPayload(payload)
	if err != nil {
		t.Fatalf("CompressPayload failed: %v", err)
	}

	// Should fit in a QR code (< 2953 bytes for alphanumeric mode)
	if len(compressed) > 2953 {
		t.Errorf("compressed payload too large for QR: %d bytes", len(compressed))
	}

	// Realistically should be under 800 bytes
	t.Logf("compressed size: %d bytes (from %d bytes SDP)", len(compressed), len(sdp))
}

func TestDecompressPayload_InvalidBase64(t *testing.T) {
	_, err := DecompressPayload("not-valid-base64!!!")
	if err == nil {
		t.Error("expected error for invalid base64")
	}
}

func TestDecompressPayload_InvalidZlib(t *testing.T) {
	// Valid base64 but not zlib
	_, err := DecompressPayload("aGVsbG8=")
	if err == nil {
		t.Error("expected error for invalid zlib")
	}
}

func TestCreateOffer_ReturnsCompressed(t *testing.T) {
	p, err := NewPeer(testConfig)
	if err != nil {
		t.Fatalf("NewPeer failed: %v", err)
	}
	defer p.Close()

	// Need a data channel for the offer to include application media
	_, err = p.CreateDataChannel("control")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	offer, err := CreateOffer(p, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	if offer == "" {
		t.Fatal("offer should not be empty")
	}

	// Should be decompressible
	payload, err := DecompressPayload(offer)
	if err != nil {
		t.Fatalf("DecompressPayload failed: %v", err)
	}

	if payload.SDP == "" {
		t.Error("SDP in offer should not be empty")
	}

	t.Logf("offer compressed size: %d bytes", len(offer))
}

func TestFullSignalingFlow(t *testing.T) {
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

	// Offerer creates data channel + offer
	_, err = offerer.CreateDataChannel("control")
	if err != nil {
		t.Fatalf("CreateDataChannel failed: %v", err)
	}

	offerCompressed, err := CreateOffer(offerer, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateOffer failed: %v", err)
	}

	// Answerer creates answer
	answerCompressed, err := CreateAnswer(answerer, offerCompressed, 3*time.Second)
	if err != nil {
		t.Fatalf("CreateAnswer failed: %v", err)
	}

	// Offerer accepts answer
	err = AcceptAnswer(offerer, answerCompressed)
	if err != nil {
		t.Fatalf("AcceptAnswer failed: %v", err)
	}

	// Both should eventually connect
	connected := make(chan struct{})
	offerer.OnStateChange(func(s PeerState) {
		if s == PeerStateConnected {
			select {
			case connected <- struct{}{}:
			default:
			}
		}
	})

	select {
	case <-connected:
		// success
	case <-time.After(10 * time.Second):
		t.Fatal("peers did not connect")
	}
}
