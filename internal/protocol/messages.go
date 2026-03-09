package protocol

import "encoding/json"

// MsgType identifies the type of message sent over WebRTC DataChannel.
type MsgType uint8

const (
	MsgTermOutput    MsgType = 0x01 // CLI -> Phone: terminal output bytes
	MsgTermInput     MsgType = 0x02 // Phone -> CLI: terminal input bytes
	MsgTermResize    MsgType = 0x03 // Phone -> CLI: resize{rows, cols}
	MsgApprovalReq   MsgType = 0x04 // CLI -> Phone: approval needed
	MsgApprovalResp  MsgType = 0x05 // Phone -> CLI: approved/denied
	MsgSessionList   MsgType = 0x06 // CLI -> Phone: list of sessions
	MsgSessionCreate MsgType = 0x07 // Phone -> CLI: create new session
	MsgSessionClose  MsgType = 0x08 // Phone -> CLI: close session
	MsgSessionSwitch MsgType = 0x09 // Phone -> CLI: switch active session
	MsgScrollback    MsgType = 0x0A // CLI -> Phone: scrollback buffer
	MsgPing          MsgType = 0x0B // bidirectional keepalive
	MsgPong          MsgType = 0x0C
	MsgError         MsgType = 0x0F // error notification
)

// Message is the envelope for all protocol messages.
type Message struct {
	Type      MsgType
	SessionID string // 16-byte UUID (no dashes) for term messages, empty for control
	Payload   []byte
}

// IsTerminalMsg returns true if the message carries raw terminal bytes
// (output or input) where Payload is raw bytes, not JSON.
func (m MsgType) IsTerminalMsg() bool {
	return m == MsgTermOutput || m == MsgTermInput || m == MsgScrollback
}

// TermResize is the payload for MsgTermResize.
type TermResize struct {
	Rows uint16 `json:"rows"`
	Cols uint16 `json:"cols"`
}

// ApprovalRequest is the payload for MsgApprovalReq.
type ApprovalRequest struct {
	ID        string `json:"id"`
	SessionID string `json:"session"`
	Prompt    string `json:"prompt"`
	Context   string `json:"context"`
	Timestamp int64  `json:"ts"`
}

// ApprovalResponse is the payload for MsgApprovalResp.
type ApprovalResponse struct {
	ID       string `json:"id"`
	Approved bool   `json:"approved"`
}

// SessionInfo describes a single PTY session.
type SessionInfo struct {
	ID      string `json:"id"`
	Command string `json:"cmd"`
	Status  string `json:"status"` // "running", "exited"
	Created int64  `json:"created"`
}

// SessionCreateReq is the payload for MsgSessionCreate.
type SessionCreateReq struct {
	Command string   `json:"cmd"`
	Args    []string `json:"args"`
}

// ErrorPayload is the payload for MsgError.
type ErrorPayload struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

// MarshalPayload encodes a typed struct into JSON bytes for use in Message.Payload.
func MarshalPayload(v interface{}) ([]byte, error) {
	return json.Marshal(v)
}

// UnmarshalPayload decodes JSON bytes from Message.Payload into a typed struct.
func UnmarshalPayload(data []byte, v interface{}) error {
	return json.Unmarshal(data, v)
}
