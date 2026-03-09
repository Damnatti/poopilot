package protocol

import (
	"strings"
	"testing"
)

func TestEncodeDecodeRoundTrip_TermOutput(t *testing.T) {
	sessionID := "abcdef0123456789" // exactly 16 bytes
	payload := []byte("hello from terminal")

	msg := Message{
		Type:      MsgTermOutput,
		SessionID: sessionID,
		Payload:   payload,
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Type != MsgTermOutput {
		t.Errorf("Type: got %d, want %d", decoded.Type, MsgTermOutput)
	}
	if decoded.SessionID != sessionID {
		t.Errorf("SessionID: got %q, want %q", decoded.SessionID, sessionID)
	}
	if string(decoded.Payload) != string(payload) {
		t.Errorf("Payload: got %q, want %q", decoded.Payload, payload)
	}
}

func TestEncodeDecodeRoundTrip_TermInput(t *testing.T) {
	sessionID := "1234567890abcdef"
	payload := []byte("y\n")

	msg := Message{
		Type:      MsgTermInput,
		SessionID: sessionID,
		Payload:   payload,
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Type != MsgTermInput {
		t.Errorf("Type mismatch")
	}
	if decoded.SessionID != sessionID {
		t.Errorf("SessionID mismatch")
	}
	if string(decoded.Payload) != "y\n" {
		t.Errorf("Payload: got %q, want %q", decoded.Payload, "y\n")
	}
}

func TestEncodeDecodeRoundTrip_Scrollback(t *testing.T) {
	sessionID := "scrollback123456"
	payload := []byte("line1\nline2\nline3\n")

	msg := Message{
		Type:      MsgScrollback,
		SessionID: sessionID,
		Payload:   payload,
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.SessionID != sessionID {
		t.Errorf("SessionID mismatch")
	}
	if string(decoded.Payload) != string(payload) {
		t.Errorf("Payload mismatch")
	}
}

func TestEncodeDecodeRoundTrip_ControlMessages(t *testing.T) {
	tests := []struct {
		name    string
		msgType MsgType
		payload []byte
	}{
		{"TermResize", MsgTermResize, []byte(`{"rows":24,"cols":80}`)},
		{"ApprovalReq", MsgApprovalReq, []byte(`{"id":"req1","session":"sess1","prompt":"Allow?","context":"...","ts":1234}`)},
		{"ApprovalResp", MsgApprovalResp, []byte(`{"id":"req1","approved":true}`)},
		{"SessionList", MsgSessionList, []byte(`[{"id":"s1","cmd":"claude","status":"running","created":1234}]`)},
		{"SessionCreate", MsgSessionCreate, []byte(`{"cmd":"claude","args":["--model","opus"]}`)},
		{"SessionClose", MsgSessionClose, []byte(`{"id":"s1"}`)},
		{"SessionSwitch", MsgSessionSwitch, []byte(`{"id":"s1"}`)},
		{"Ping", MsgPing, nil},
		{"Pong", MsgPong, nil},
		{"Error", MsgError, []byte(`{"code":"ERR","message":"something broke"}`)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := Message{
				Type:    tt.msgType,
				Payload: tt.payload,
			}

			encoded, err := Encode(msg)
			if err != nil {
				t.Fatalf("Encode failed: %v", err)
			}

			decoded, err := Decode(encoded)
			if err != nil {
				t.Fatalf("Decode failed: %v", err)
			}

			if decoded.Type != tt.msgType {
				t.Errorf("Type: got %d, want %d", decoded.Type, tt.msgType)
			}
			if string(decoded.Payload) != string(tt.payload) {
				t.Errorf("Payload: got %q, want %q", decoded.Payload, tt.payload)
			}
		})
	}
}

func TestEncode_InvalidSessionID_TooShort(t *testing.T) {
	msg := Message{
		Type:      MsgTermOutput,
		SessionID: "short",
		Payload:   []byte("data"),
	}

	_, err := Encode(msg)
	if err != ErrInvalidSessionID {
		t.Errorf("expected ErrInvalidSessionID, got %v", err)
	}
}

func TestEncode_InvalidSessionID_TooLong(t *testing.T) {
	msg := Message{
		Type:      MsgTermOutput,
		SessionID: "this-is-way-too-long-session-id",
		Payload:   []byte("data"),
	}

	_, err := Encode(msg)
	if err != ErrInvalidSessionID {
		t.Errorf("expected ErrInvalidSessionID, got %v", err)
	}
}

func TestEncode_PayloadTooLarge(t *testing.T) {
	msg := Message{
		Type:    MsgPing,
		Payload: make([]byte, MaxPayloadSize+1),
	}

	_, err := Encode(msg)
	if err != ErrPayloadTooLarge {
		t.Errorf("expected ErrPayloadTooLarge, got %v", err)
	}
}

func TestDecode_TooShort(t *testing.T) {
	_, err := Decode([]byte{0x01})
	if err != ErrMessageTooShort {
		t.Errorf("expected ErrMessageTooShort, got %v", err)
	}
}

func TestDecode_TruncatedPayload(t *testing.T) {
	// Header says 100 bytes payload but only 5 provided
	data := []byte{0x0B, 0x00, 0x64, 0x01, 0x02, 0x03, 0x04, 0x05}
	_, err := Decode(data)
	if err != ErrMessageTooShort {
		t.Errorf("expected ErrMessageTooShort, got %v", err)
	}
}

func TestDecode_InvalidMsgType(t *testing.T) {
	data := []byte{0xFF, 0x00, 0x00}
	_, err := Decode(data)
	if err == nil || !strings.Contains(err.Error(), "invalid message type") {
		t.Errorf("expected invalid message type error, got %v", err)
	}
}

func TestDecode_TerminalMsg_MissingSessionID(t *testing.T) {
	// MsgTermOutput with only 5 bytes of payload (need at least 16 for session ID)
	data := []byte{0x01, 0x00, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05}
	_, err := Decode(data)
	if err != ErrInvalidSessionID {
		t.Errorf("expected ErrInvalidSessionID, got %v", err)
	}
}

func TestBinaryFormat_HeaderLayout(t *testing.T) {
	msg := Message{
		Type:    MsgPing,
		Payload: []byte{0xDE, 0xAD},
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	// Byte 0: type
	if encoded[0] != byte(MsgPing) {
		t.Errorf("byte 0: got 0x%02x, want 0x%02x", encoded[0], byte(MsgPing))
	}
	// Bytes 1-2: payload length (big-endian)
	if encoded[1] != 0x00 || encoded[2] != 0x02 {
		t.Errorf("payload length: got [0x%02x, 0x%02x], want [0x00, 0x02]", encoded[1], encoded[2])
	}
	// Bytes 3+: payload
	if encoded[3] != 0xDE || encoded[4] != 0xAD {
		t.Errorf("payload bytes mismatch")
	}
}

func TestEncodeDecodeRoundTrip_EmptyPayload(t *testing.T) {
	msg := Message{
		Type:    MsgPing,
		Payload: nil,
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if decoded.Type != MsgPing {
		t.Errorf("Type mismatch")
	}
	if len(decoded.Payload) != 0 {
		t.Errorf("expected empty payload, got %d bytes", len(decoded.Payload))
	}
}

func TestEncodeDecodeRoundTrip_MaxPayload(t *testing.T) {
	payload := make([]byte, MaxPayloadSize)
	for i := range payload {
		payload[i] = byte(i % 256)
	}

	msg := Message{
		Type:    MsgError,
		Payload: payload,
	}

	encoded, err := Encode(msg)
	if err != nil {
		t.Fatalf("Encode failed: %v", err)
	}

	decoded, err := Decode(encoded)
	if err != nil {
		t.Fatalf("Decode failed: %v", err)
	}

	if len(decoded.Payload) != MaxPayloadSize {
		t.Errorf("payload length: got %d, want %d", len(decoded.Payload), MaxPayloadSize)
	}
}

func TestIsTerminalMsg(t *testing.T) {
	terminal := []MsgType{MsgTermOutput, MsgTermInput, MsgScrollback}
	nonTerminal := []MsgType{MsgTermResize, MsgApprovalReq, MsgApprovalResp, MsgSessionList, MsgSessionCreate, MsgSessionClose, MsgSessionSwitch, MsgPing, MsgPong, MsgError}

	for _, mt := range terminal {
		if !mt.IsTerminalMsg() {
			t.Errorf("expected 0x%02x to be terminal msg", byte(mt))
		}
	}
	for _, mt := range nonTerminal {
		if mt.IsTerminalMsg() {
			t.Errorf("expected 0x%02x to NOT be terminal msg", byte(mt))
		}
	}
}
