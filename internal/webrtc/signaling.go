package webrtc

import (
	"bytes"
	"compress/zlib"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"github.com/pion/webrtc/v4"
)

// TURNCredentials holds ephemeral TURN auth info.
type TURNCredentials struct {
	URLs       []string `json:"u"`
	Username   string   `json:"n"`
	Credential string   `json:"c"`
}

// SignalingPayload is the data encoded in the QR code.
type SignalingPayload struct {
	SDP         string           `json:"s"`
	STUNServers []string         `json:"u,omitempty"`
	TURN        *TURNCredentials `json:"t,omitempty"`
}

// CreateOffer creates an SDP offer with all ICE candidates gathered.
func CreateOffer(p *Peer, gatherTimeout time.Duration) (string, error) {
	return CreateOfferWithTURN(p, gatherTimeout, nil)
}

// CreateOfferWithTURN creates an SDP offer and embeds TURN credentials in the payload.
func CreateOfferWithTURN(p *Peer, gatherTimeout time.Duration, turn *TURNCredentials) (string, error) {
	offer, err := p.pc.CreateOffer(nil)
	if err != nil {
		return "", fmt.Errorf("create offer: %w", err)
	}

	if err := p.pc.SetLocalDescription(offer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}

	// Wait for ICE gathering to complete or timeout
	gatherDone := webrtc.GatheringCompletePromise(p.pc)
	select {
	case <-gatherDone:
	case <-time.After(gatherTimeout):
	}

	// Get the complete local description with candidates
	localDesc := p.pc.LocalDescription()
	if localDesc == nil {
		return "", fmt.Errorf("local description is nil")
	}

	payload := SignalingPayload{
		SDP:  localDesc.SDP,
		TURN: turn,
	}

	return CompressPayload(payload)
}

// AcceptAnswer decodes and applies an SDP answer from the phone.
func AcceptAnswer(p *Peer, compressed string) error {
	payload, err := DecompressPayload(compressed)
	if err != nil {
		return fmt.Errorf("decompress answer: %w", err)
	}

	answer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeAnswer,
		SDP:  payload.SDP,
	}

	if err := p.pc.SetRemoteDescription(answer); err != nil {
		return fmt.Errorf("set remote description: %w", err)
	}

	return nil
}

// CreateAnswer creates an SDP answer (used by the phone side / tests).
func CreateAnswer(p *Peer, offerCompressed string, gatherTimeout time.Duration) (string, error) {
	payload, err := DecompressPayload(offerCompressed)
	if err != nil {
		return "", fmt.Errorf("decompress offer: %w", err)
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.SDPTypeOffer,
		SDP:  payload.SDP,
	}

	if err := p.pc.SetRemoteDescription(offer); err != nil {
		return "", fmt.Errorf("set remote description: %w", err)
	}

	answer, err := p.pc.CreateAnswer(nil)
	if err != nil {
		return "", fmt.Errorf("create answer: %w", err)
	}

	if err := p.pc.SetLocalDescription(answer); err != nil {
		return "", fmt.Errorf("set local description: %w", err)
	}

	gatherDone := webrtc.GatheringCompletePromise(p.pc)
	select {
	case <-gatherDone:
	case <-time.After(gatherTimeout):
	}

	localDesc := p.pc.LocalDescription()
	if localDesc == nil {
		return "", fmt.Errorf("local description is nil")
	}

	answerPayload := SignalingPayload{
		SDP: localDesc.SDP,
	}

	return CompressPayload(answerPayload)
}

// CompressPayload compresses a SignalingPayload to a base64 string.
func CompressPayload(payload SignalingPayload) (string, error) {
	jsonData, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}

	var buf bytes.Buffer
	w, err := zlib.NewWriterLevel(&buf, zlib.BestCompression)
	if err != nil {
		return "", err
	}
	if _, err := w.Write(jsonData); err != nil {
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}

	return base64.RawURLEncoding.EncodeToString(buf.Bytes()), nil
}

// DecompressPayload decompresses a base64 string to a SignalingPayload.
func DecompressPayload(compressed string) (SignalingPayload, error) {
	data, err := base64.RawURLEncoding.DecodeString(compressed)
	if err != nil {
		return SignalingPayload{}, fmt.Errorf("base64 decode: %w", err)
	}

	r, err := zlib.NewReader(bytes.NewReader(data))
	if err != nil {
		return SignalingPayload{}, fmt.Errorf("zlib reader: %w", err)
	}
	defer r.Close()

	jsonData, err := io.ReadAll(r)
	if err != nil {
		return SignalingPayload{}, fmt.Errorf("zlib read: %w", err)
	}

	var payload SignalingPayload
	if err := json.Unmarshal(jsonData, &payload); err != nil {
		return SignalingPayload{}, fmt.Errorf("json unmarshal: %w", err)
	}

	return payload, nil
}
