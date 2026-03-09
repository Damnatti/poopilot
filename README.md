<p align="center">
  <br>
  <strong style="font-size: 48px">💩</strong>
  <br>
</p>

<h1 align="center">poopilot</h1>

<p align="center">
  <strong>Control your AI coding agents from the toilet.</strong>
  <br>
  Monitor & approve Claude CLI, Codex, and other terminal AI tools from your phone over P2P WebRTC.
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

**poopilot** wraps your AI CLI tool in a PTY, displays a QR code, and lets you monitor and control the session from your phone over a direct peer-to-peer WebRTC connection. No accounts. No bullshit.

Scan. Connect. Approve from anywhere.

## Install

### Homebrew (macOS)

```bash
brew install denismelnikov/tap/poopilot
```

### Go

```bash
go install github.com/Damnatti/poopilot/cmd/poopilot@latest
```

### From source

```bash
git clone https://github.com/Damnatti/poopilot.git
cd poopilot
make build
# binary is in ./bin/poopilot
```

## Usage

```bash
# Same network (WiFi / VPN) — scan QR from your phone
poopilot run claude

# Any network — uses a lightweight relay for the initial handshake
poopilot run --relay https://poopilot-relay.workers.dev claude

# Pass args to the wrapped tool
poopilot run -- claude --model opus
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
│             │                          │             │
│  poopilot   │  ┌───────────────────┐   │  PWA with   │
│  ┌───────┐  │  │ PTY + WebRTC      │   │  xterm.js   │
│  │ claude│◄─┤──┤ bridge            │   │             │
│  │  CLI  │  │  └───────────────────┘   │  approve /  │
│  └───────┘  │                          │  deny UI    │
└─────────────┘                          └─────────────┘
       ▲                                        ▲
       └──── optional relay for handshake ──────┘
             (only SDP exchange, ~2KB)
             terminal data always P2P
```

1. `poopilot run claude` spawns Claude CLI inside a PTY
2. A QR code appears in your terminal
3. Phone scans QR, establishes a WebRTC P2P connection
4. Terminal I/O streams over DataChannels — your phone becomes a live mirror
5. When Claude asks "Allow this action?", your phone vibrates and shows approve/deny buttons
6. You tap approve. Claude continues. You continue... whatever you were doing.

**Terminal data always flows directly** between your machine and phone via encrypted WebRTC. The optional `--relay` flag only helps with the initial handshake (~2KB of connection metadata) so your phone doesn't need to be on the same network.

## Network Modes

| Mode | Flag | Phone network | How it works |
|------|------|---------------|--------------|
| **Local** | *(default)* | Same WiFi / VPN | QR points to local IP, no external services |
| **Relay** | `--relay <url>` | Any network | Handshake goes through a [Cloudflare Worker](relay/), terminal data still P2P |

### Setting up a relay

The relay is a tiny Cloudflare Worker (~50 lines) that stores offer/answer blobs in KV with a 5-minute TTL. No terminal data ever touches it.

**Guided setup:**

```bash
poopilot setup
```

This walks you through deploying the relay and configuring the env var. Or do it manually:

**One-click deploy:**

[![Deploy to Cloudflare Workers](https://deploy.workers.cloudflare.com/button)](https://deploy.workers.cloudflare.com/?url=https://github.com/Damnatti/poopilot/tree/main/relay)

Then save your relay URL:

```bash
echo 'export POOPILOT_RELAY=https://your-relay.workers.dev' >> ~/.zshrc
source ~/.zshrc
poopilot setup  # verify it works
```

## Requirements

- macOS or Linux (arm64/amd64)
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
  relay/           HTTP client for cloud signaling relay
  qr/              QR code terminal rendering
  cli/             Cobra commands
web/               Embedded PWA (xterm.js + vanilla JS)
relay/             Cloudflare Worker for cross-network signaling
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

Approval detection has built-in patterns for Claude and Codex prompts, plus generic yes/no detection.

## FAQ

**Q: Does this work over the internet?**
A: Yes! Use `--relay` flag to pair from any network. The relay only handles the initial handshake (~2KB). All terminal data flows directly between your devices via WebRTC.

**Q: Is it secure?**
A: All terminal data flows over WebRTC DataChannels with mandatory DTLS encryption. The relay (if used) only sees encrypted connection metadata, never your terminal content.

**Q: Can I use it without the phone?**
A: Yes — `poopilot run claude` works as a normal terminal wrapper even without connecting a phone.

**Q: Why "poopilot"?**
A: You know exactly why.

## Development

```bash
make test          # Run all tests with race detector
make test-cover    # Tests + HTML coverage report
make build         # Build binary to ./bin/poopilot
make install       # Install to $GOPATH/bin
make lint          # Run golangci-lint
```

## License

MIT — see [LICENSE](LICENSE).
