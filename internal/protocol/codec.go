package protocol

import (
	"encoding/binary"
	"errors"
	"fmt"
)

const (
	// HeaderSize is type(1) + payload length(2) = 3 bytes.
	HeaderSize = 3
	// SessionIDSize is 16 bytes (UUID without dashes).
	SessionIDSize = 16
	// MaxPayloadSize is the maximum payload size (64KB - 1).
	MaxPayloadSize = 65535
)

var (
	ErrMessageTooShort  = errors.New("message too short")
	ErrPayloadTooLarge  = errors.New("payload exceeds max size")
	ErrInvalidSessionID = errors.New("session ID must be 16 bytes for terminal messages")
	ErrInvalidMsgType   = errors.New("invalid message type")
)

// validMsgTypes is the set of known message types.
var validMsgTypes = map[MsgType]bool{
	MsgTermOutput:    true,
	MsgTermInput:     true,
	MsgTermResize:    true,
	MsgApprovalReq:   true,
	MsgApprovalResp:  true,
	MsgSessionList:   true,
	MsgSessionCreate: true,
	MsgSessionClose:  true,
	MsgSessionSwitch: true,
	MsgScrollback:    true,
	MsgPing:          true,
	MsgPong:          true,
	MsgError:         true,
}

// Encode serializes a Message into binary format:
//
//	[1 byte type][2 bytes payload length][N bytes payload]
//
// For terminal messages (TermOutput, TermInput, Scrollback), the payload is:
//
//	[16 bytes session ID][raw terminal bytes]
//
// For all other messages, the payload is the raw Payload bytes (typically JSON).
func Encode(msg Message) ([]byte, error) {
	var payload []byte

	if msg.Type.IsTerminalMsg() {
		if len(msg.SessionID) != SessionIDSize {
			return nil, ErrInvalidSessionID
		}
		payload = make([]byte, SessionIDSize+len(msg.Payload))
		copy(payload[:SessionIDSize], []byte(msg.SessionID))
		copy(payload[SessionIDSize:], msg.Payload)
	} else {
		payload = msg.Payload
	}

	if len(payload) > MaxPayloadSize {
		return nil, ErrPayloadTooLarge
	}

	buf := make([]byte, HeaderSize+len(payload))
	buf[0] = byte(msg.Type)
	binary.BigEndian.PutUint16(buf[1:3], uint16(len(payload)))
	copy(buf[HeaderSize:], payload)

	return buf, nil
}

// Decode deserializes binary data into a Message.
func Decode(data []byte) (Message, error) {
	if len(data) < HeaderSize {
		return Message{}, ErrMessageTooShort
	}

	msgType := MsgType(data[0])
	if !validMsgTypes[msgType] {
		return Message{}, fmt.Errorf("%w: 0x%02x", ErrInvalidMsgType, byte(msgType))
	}

	payloadLen := binary.BigEndian.Uint16(data[1:3])
	if len(data) < HeaderSize+int(payloadLen) {
		return Message{}, ErrMessageTooShort
	}

	payload := data[HeaderSize : HeaderSize+int(payloadLen)]

	msg := Message{
		Type: msgType,
	}

	if msgType.IsTerminalMsg() {
		if len(payload) < SessionIDSize {
			return Message{}, ErrInvalidSessionID
		}
		msg.SessionID = string(payload[:SessionIDSize])
		msg.Payload = payload[SessionIDSize:]
	} else {
		msg.Payload = payload
	}

	return msg, nil
}
