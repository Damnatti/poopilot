<p align="center">
  <br>
  <strong style="font-size: 48px">💩</strong>
  <br>
</p>

<h1 align="center">poopilot</h1>

<p align="center">
  <strong>Control your AI coding agents from the toilet.</strong>
  <br>
  Monitor & approve Claude CLI, Codex, and other terminal AI tools from your phone — zero servers, pure P2P.
</p>

<p align="center">
  <a href="#install">Install</a> &bull;
  <a href="#how-it-works">How It Works</a> &bull;
  <a href="#usage">Usage</a> &bull;
  <a href="#why">Why</a>
</p>

---

You kick off a Claude Code session. It's churning through your codebase, reading files, writing code, running tests. You need to step away. Maybe grab coffee. Maybe answer nature's call.

But Claude needs approval to run `rm -rf node_modules && npm install`. Your terminal is on your desk. You're... not.

**poopilot** wraps your AI CLI tool in a PTY, displays a QR code, and lets you monitor and control the session from your phone over a direct peer-to-peer WebRTC connection. No cloud servers. No accounts. No bullshit.

Scan. Connect. Approve from anywhere in your apartment.

## Install

### Homebrew (macOS)

```bash
brew install denismelnikov/tap/poopilot
```

### Go

```bash
go install github.com/denismelnikov/poopilot/cmd/poopilot@latest
```

### From source

```bash
git clone https://github.com/denismelnikov/poopilot.git
cd poopilot
make build
# binary is in ./bin/poopilot
```

## Usage

```bash
# Wrap any CLI tool
poopilot run claude
poopilot run codex
poopilot run aider
poopilot run -- claude --model opus

# That's it. A QR code appears. Scan it with your phone.
```

Your terminal works exactly as before — same input, same output. But now your phone is a second screen that can:

- See everything the terminal sees in real-time
- Approve or deny actions when the AI asks
- Get vibration alerts when approval is needed
- Reconnect anytime by rescanning the QR

## How It Works

```
┌─────────────┐       P2P WebRTC        ┌─────────────┐
│  Your Mac   │◄══════════════════════►  │  Your Phone │
│             │    (same WiFi/VPN)       │             │
│  poopilot   │                          │  PWA with   │
│  ┌───────┐  │  ┌──────────────────┐    │  xterm.js   │
│  │ claude│◄─┤──┤ PTY + WebRTC     │    │             │
│  │  CLI  │  │  │ bridge           │    │  approve /  │
│  └───────┘  │  └──────────────────┘    │  deny UI    │
└─────────────┘                          └─────────────┘
```

1. `poopilot run claude` spawns Claude CLI inside a PTY
2. A local HTTP server starts and displays a QR code with the URL
3. Phone scans QR, loads a PWA, establishes a WebRTC P2P connection
4. Terminal I/O streams over DataChannels — your phone becomes a live mirror
5. When Claude asks "Allow this action?", your phone vibrates and shows approve/deny buttons
6. You tap approve. Claude continues. You continue... whatever you were doing.

**Zero servers involved.** The HTTP server is local (for the initial handshake only). All terminal data flows directly between your machine and phone via WebRTC with DTLS encryption.

## Requirements

- macOS or Linux (arm64/amd64)
- Phone on the same network as your machine (WiFi or VPN like Tailscale)
- Go 1.22+ (if building from source)

## Architecture

The project is structured as a single binary with an embedded PWA:

```
cmd/poopilot/      CLI entry point
internal/
  pty/             PTY session management + ring buffer scrollback
  webrtc/          pion/webrtc peer + zlib-compressed signaling
  protocol/        Binary message protocol over DataChannels
  bridge/          PTY ↔ WebRTC router + approval detection
  approval/        Regex-based y/n prompt detection
  qr/              QR code terminal rendering
  cli/             Cobra commands
web/               Embedded PWA (xterm.js + vanilla JS)
```

See [ARCHITECTURE.md](ARCHITECTURE.md) for the full deep dive.

## Supported AI Tools

Works with anything that runs in a terminal:

| Tool | Status |
|------|--------|
| Claude CLI (`claude`) | Tested |
| OpenAI Codex CLI (`codex`) | Tested |
| Aider (`aider`) | Should work |
| GitHub Copilot CLI | Should work |
| Any interactive CLI | Should work |

Approval detection has built-in patterns for Claude and Codex prompts, plus generic yes/no detection. Custom patterns can be added.

## FAQ

**Q: Does this work over the internet?**
A: It works over any network where your phone can reach your machine — same WiFi, Tailscale, WireGuard, etc. It uses Google STUN servers for NAT traversal, so it may work across networks too, but same-network is most reliable.

**Q: Is it secure?**
A: All data flows over WebRTC DataChannels with mandatory DTLS encryption. No data touches any server. The signaling happens locally over your LAN.

**Q: Can I use it without the phone?**
A: Yes — `poopilot run claude` works as a normal terminal wrapper even without connecting a phone. The phone part is optional.

**Q: Why "poopilot"?**
A: You know exactly why.

## Development

```bash
make test          # Run all tests with race detector
make test-cover    # Tests + HTML coverage report
make build         # Build binary to ./bin/poopilot
make lint          # Run golangci-lint
```

## License

MIT — see [LICENSE](LICENSE).
