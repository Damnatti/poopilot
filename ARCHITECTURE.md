# Poopilot — Architecture & Implementation Plan

## Overview

CLI tool: `poopilot run claude` → spawns CLI in PTY → shows QR in terminal → phone scans → P2P WebRTC → live terminal + approve/deny from phone.

Zero servers. Pure P2P via WebRTC + Google STUN.

---

## Project Structure

```
poopilot/
├── go.mod
├── go.sum
├── Makefile
├── cmd/
│   └── poopilot/
│       └── main.go                 # CLI entrypoint
├── internal/
│   ├── cli/
│   │   ├── root.go                 # Root cobra command
│   │   ├── run.go                  # `poopilot run <tool>` subcommand
│   │   ├── status.go               # `poopilot status`
│   │   └── cli_test.go
│   ├── pty/
│   │   ├── session.go              # Single PTY session + RingBuffer
│   │   ├── manager.go              # Multi-session manager
│   │   ├── session_test.go
│   │   └── manager_test.go
│   ├── webrtc/
│   │   ├── peer.go                 # PeerConnection setup, ICE, STUN
│   │   ├── signaling.go            # Offer/answer SDP, QR encoding
│   │   ├── channel.go              # DataChannel management
│   │   ├── peer_test.go
│   │   ├── signaling_test.go
│   │   └── channel_test.go
│   ├── protocol/
│   │   ├── messages.go             # All message types
│   │   ├── codec.go                # Binary encode/decode
│   │   ├── messages_test.go
│   │   └── codec_test.go
│   ├── approval/
│   │   ├── detector.go             # Yes/no prompt detection
│   │   ├── detector_test.go
│   │   └── testdata/
│   │       ├── claude_approve.txt
│   │       ├── codex_approve.txt
│   │       └── generic_yesno.txt
│   ├── bridge/
│   │   ├── bridge.go               # PTY <-> WebRTC glue
│   │   └── bridge_test.go
│   └── qr/
│       ├── render.go               # QR terminal rendering
│       └── render_test.go
├── web/                            # PWA (embedded via go:embed)
│   ├── index.html
│   ├── manifest.json
│   ├── sw.js                       # Service worker
│   ├── css/
│   │   └── app.css
│   ├── js/
│   │   ├── app.js                  # Main app logic, session tabs
│   │   ├── rtc.js                  # WebRTC client
│   │   ├── terminal.js             # xterm.js wrapper
│   │   ├── approval.js             # Approve/deny UI + haptic
│   │   ├── protocol.js             # Wire protocol (mirrors Go)
│   │   ├── scanner.js              # QR camera scanner
│   │   └── notifications.js        # Notification API + vibration
│   └── vendor/
│       ├── xterm.min.js
│       ├── xterm.min.css
│       └── xterm-addon-fit.min.js
└── ARCHITECTURE.md
```

---

## Key Data Structures

### Protocol Messages (binary over WebRTC DataChannel)

```
Byte 0:       MsgType (uint8)
Bytes 1-2:    Payload length (uint16 big-endian)
Bytes 3-N:    Payload

For MsgTermOutput/MsgTermInput:
  Bytes 3-18:  Session ID (16 bytes UUID)
  Bytes 19-N:  Raw terminal bytes

For all other types:
  Bytes 3-N:   JSON-encoded struct
```

Message types:
- `0x01` TermOutput — CLI → Phone: terminal output
- `0x02` TermInput — Phone → CLI: terminal input
- `0x03` TermResize — Phone → CLI: resize{rows, cols}
- `0x04` ApprovalReq — CLI → Phone: approval needed
- `0x05` ApprovalResp — Phone → CLI: approved/denied
- `0x06` SessionList — CLI → Phone: list of sessions
- `0x07` SessionCreate — Phone → CLI: create new session
- `0x08` SessionClose — Phone → CLI: close session
- `0x09` SessionSwitch — Phone → CLI: switch active session
- `0x0A` Scrollback — CLI → Phone: scrollback buffer on connect
- `0x0B` Ping / `0x0C` Pong — keepalive
- `0x0F` Error

### PTY Session

```go
type Session struct {
    ID        string
    Command   string
    Args      []string
    pty       *os.File
    output    *RingBuffer   // last 64KB for scrollback
    onOutput  func([]byte)
    onExit    func(int)
}
```

RingBuffer: circular 64KB buffer for reconnection scrollback.

### WebRTC Peer

```go
type Peer struct {
    pc         *webrtc.PeerConnection
    controlCh  *webrtc.DataChannel        // signaling, session mgmt
    dataChs    map[string]*webrtc.DataChannel  // "term:<sessionID>"
    state      PeerState
}
```

---

## Signaling Flow (QR-based, zero servers)

```
CLI (Go)                                Phone (PWA)
────────                                ──────────
1. Create PeerConnection
2. Create DataChannel("control")
3. CreateOffer(), gather ICE (3s)
4. Compress(SDP + candidates + STUN)
5. Encode as URL: http://local-ip:PORT/pair#<offer>
6. Render QR in terminal
7. Start temp HTTP server for PWA
                                        8.  Scan QR → opens PWA URL
                                        9.  Extract offer from URL fragment
                                        10. setRemoteDescription(offer)
                                        11. createAnswer(), gather ICE
                                        12. POST answer to http://cli-ip:PORT/answer
13. Receive answer, setRemoteDescription
14. ICE connectivity → P2P connected
    ←──── WebRTC DataChannel open ────→
15. Send MsgSessionList             →   16. Display sessions
                                        17. User taps → MsgSessionSwitch
18. Send MsgScrollback              →   19. Render in xterm.js
```

Fallback if HTTP POST fails (different network): PWA shows answer as copyable text, user pastes into CLI terminal.

---

## Terminal I/O Flow (steady state)

```
PTY stdout → Session.onOutput → Detector.Scan → MsgTermOutput → Phone xterm.write()
                                     │
                                     ├─ prompt detected → MsgApprovalReq → show buttons + vibrate
                                     │                    MsgApprovalResp ← user taps
                                     │                    write "y\n" or "n\n" to PTY
                                     │
Phone xterm.onData → MsgTermInput → Session.Write(stdin)
Phone xterm.onResize → MsgTermResize → Session.Resize()
```

---

## Approval Detector

Regex patterns for:
- Claude: `(?i)(do you want to proceed|allow|deny|approve|y/n|yes/no|\[Y/n\]|\[y/N\])`
- Claude tool use: `(?i)(allow once|allow always|deny)`
- Codex: `(?i)(approve|reject|confirm)`
- Generic: `(?i)(\? \[Y/n\]|\? \[y/N\]|proceed\?|continue\?|accept\?)`

Scans last 512 bytes of output. Deduplicates by line offset.

---

## Edge Cases

### NAT Traversal Failure
Google STUN works ~85% of cases. On failure: show warning, suggest same LAN. Future: `--turn-server` flag.

### Reconnection
PTY keeps running. RingBuffer preserves last 64KB. CLI re-shows QR on disconnect. Phone reconnects, gets MsgScrollback.

### Large Output (cat bigfile.txt)
5ms coalesce timer, 16KB max per message. Backpressure: if DataChannel buffer > 1MB, drop oldest unsent batches.

### QR Size
Data-channel-only SDP ~300 bytes + ICE candidates ~200 bytes → zlib compress → ~350 bytes → base64 ~465 bytes + URL prefix ~35 bytes = **~500 bytes total**. Fits QR Version 10.

### Security
- DTLS encryption on all WebRTC traffic
- QR contains DTLS fingerprint — MITM requires physical QR access
- HTTP server: LAN only, 30s pairing window, accepts one POST then shuts down

---

## Dependencies (go.mod)

```
module github.com/denismelnikov/poopilot

go 1.22

require (
    github.com/pion/webrtc/v4
    github.com/creack/pty
    github.com/skip2/go-qrcode
    github.com/spf13/cobra
    github.com/google/uuid
    github.com/stretchr/testify  // tests
)
```

---

## Implementation Order (TDD)

### Phase 1: Protocol + Codec (no deps)
1. `internal/protocol/messages.go` — types and structs
2. `internal/protocol/codec.go` — binary encode/decode
3. Tests
4. `web/js/protocol.js` — JS mirror

### Phase 2: PTY Management (parallel with Phase 1)
1. RingBuffer → Session → Manager
2. Tests with `echo`, `cat`, `sh`

### Phase 3: Approval Detector (parallel)
1. `internal/approval/detector.go`
2. Testdata samples
3. Tests

### Phase 4: WebRTC Layer (depends on Phase 1)
1. Peer → Channel → Signaling → QR render
2. Loopback tests (two peers, same process)

### Phase 5: Bridge (depends on 1-4)
1. Wire PTY ↔ WebRTC
2. Integration tests with mocks

### Phase 6: CLI + PWA (depends on 5)
1. Cobra commands
2. go:embed PWA
3. HTTP server for pairing
4. All PWA JS modules
5. End-to-end testing

---

## Makefile

```makefile
build:
	go build -o bin/poopilot ./cmd/poopilot

test:
	go test ./internal/... -v -race -count=1

test-cover:
	go test ./internal/... -coverprofile=coverage.out
	go tool cover -html=coverage.out

lint:
	golangci-lint run ./...

run:
	go run ./cmd/poopilot run claude

clean:
	rm -rf bin/ coverage.out
```
