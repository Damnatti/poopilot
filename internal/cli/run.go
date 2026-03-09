package cli

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"golang.org/x/term"

	"github.com/Damnatti/poopilot/internal/approval"
	"github.com/Damnatti/poopilot/internal/bridge"
	"github.com/Damnatti/poopilot/internal/pty"
	"github.com/Damnatti/poopilot/internal/qr"
	"github.com/Damnatti/poopilot/internal/relay"
	rtc "github.com/Damnatti/poopilot/internal/webrtc"
	"github.com/spf13/cobra"
)

// WebFS is set by main.go via go:embed.
var WebFS fs.FS

func newRunCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "run [command] [args...]",
		Short: "Run a CLI tool wrapped with poopilot",
		Long:  "Spawns the given command in a PTY, shows a QR code for phone pairing, and bridges terminal I/O over WebRTC.",
		Example: `  poopilot run claude
  poopilot run claude -- --model opus
  poopilot run --relay https://poopilot-relay.workers.dev claude`,
		Args: cobra.MinimumNArgs(1),
		RunE: runCommand,
	}
}

// peerManager handles creating new WebRTC peers for each connection attempt.
type peerManager struct {
	mu       sync.Mutex
	mgr      *pty.Manager
	detector *approval.Detector
	peer     *rtc.Peer
	br       *bridge.Bridge
	ctx      context.Context
}

func (pm *peerManager) newOffer() (string, error) {
	pm.mu.Lock()
	defer pm.mu.Unlock()

	// Close previous peer if any
	if pm.peer != nil {
		pm.peer.Close()
	}

	peer, err := rtc.NewPeer(rtc.PeerConfig{})
	if err != nil {
		return "", err
	}

	_, err = peer.CreateDataChannel("control")
	if err != nil {
		peer.Close()
		return "", err
	}

	offer, err := rtc.CreateOffer(peer, 5*time.Second)
	if err != nil {
		peer.Close()
		return "", err
	}

	pm.peer = peer

	// Create new bridge for this peer
	pm.br = bridge.New(pm.mgr, peer, pm.detector)
	pm.br.Start(pm.ctx)

	// Attach all existing sessions
	for _, info := range pm.mgr.List() {
		pm.br.AttachSession(info.ID)
	}

	return offer, nil
}

func (pm *peerManager) acceptAnswer(answer string) error {
	pm.mu.Lock()
	peer := pm.peer
	pm.mu.Unlock()

	if peer == nil {
		return fmt.Errorf("no pending offer")
	}
	return rtc.AcceptAnswer(peer, answer)
}

func (pm *peerManager) close() {
	pm.mu.Lock()
	defer pm.mu.Unlock()
	if pm.peer != nil {
		pm.peer.Close()
	}
}

func runCommand(cmd *cobra.Command, args []string) error {
	command := args[0]
	var cmdArgs []string
	if len(args) > 1 {
		cmdArgs = args[1:]
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Handle signals
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Create PTY manager
	mgr := pty.NewManager(8)
	defer mgr.CloseAll()

	// Create first session
	sess, err := mgr.Create(command, cmdArgs)
	if err != nil {
		return fmt.Errorf("failed to start %s: %w", command, err)
	}

	// Peer manager handles WebRTC lifecycle (supports reconnect)
	pm := &peerManager{
		mgr:      mgr,
		detector: approval.NewDetector(),
		ctx:      ctx,
	}
	defer pm.close()

	// Get local IP
	localIP := getLocalIP()

	// Start HTTP server (always, for LAN fallback)
	srv := startHTTPServer(localIP, port, pm)
	defer srv.Close()

	localURL := fmt.Sprintf("http://%s:%d", localIP, port)

	// Banner
	fmt.Println()
	fmt.Println("  💩 \033[1mpoopilot\033[0m " + Version)
	fmt.Println()
	fmt.Println("  \033[2mWrapping:\033[0m  " + command)
	fmt.Println("  \033[2mNetwork:\033[0m   " + localIP + ":" + fmt.Sprint(port))
	fmt.Println("  \033[2mProtocol:\033[0m  P2P WebRTC (DTLS encrypted)")
	fmt.Println()

	if relayURL != "" {
		// Cloud relay mode
		err := startRelaySignaling(ctx, pm, relayURL, localURL)
		if err != nil {
			fmt.Fprintf(os.Stderr, "  \033[31mRelay error: %v\033[0m\n", err)
			fmt.Println("  \033[2mFalling back to local-only mode.\033[0m")
			printLocalQR(localURL)
		}
	} else {
		printLocalQR(localURL)
	}

	// Put local terminal in raw mode
	fd := int(os.Stdin.Fd())
	if term.IsTerminal(fd) {
		oldState, err := term.MakeRaw(fd)
		if err == nil {
			defer term.Restore(fd, oldState)
		}
	}

	// Pipe PTY output to local stdout
	sess.OnOutput(func(data []byte) {
		os.Stdout.Write(data)
	})

	// Pipe local stdin to PTY
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := os.Stdin.Read(buf)
			if err != nil {
				return
			}
			sess.Write(buf[:n])
		}
	}()

	// Wait for session to exit or context cancel
	select {
	case <-sess.Done():
		fmt.Fprintf(os.Stderr, "\r\n[poopilot] Process exited.\r\n")
	case <-ctx.Done():
	}

	return nil
}

func stableRoomID() string {
	hostname, _ := os.Hostname()
	username := os.Getenv("USER")
	if username == "" {
		username = os.Getenv("USERNAME")
	}
	raw := fmt.Sprintf("poopilot:%s:%s", hostname, username)
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte(raw)))
	return hash[:12]
}

func startRelaySignaling(ctx context.Context, pm *peerManager, relayURL, localURL string) error {
	roomID := stableRoomID()

	// Create offer
	offer, err := pm.newOffer()
	if err != nil {
		return fmt.Errorf("create offer: %w", err)
	}

	// Upload offer to relay
	if err := relay.PostOffer(relayURL, roomID, offer); err != nil {
		return fmt.Errorf("upload offer: %w", err)
	}

	// QR URL points to relay with room ID
	pairURL := fmt.Sprintf("%s/#room=%s", relayURL, roomID)

	fmt.Println("  \033[33mScan QR with your phone to connect:\033[0m")
	fmt.Println("  \033[2m(works from any network)\033[0m")
	fmt.Println()

	qrStr, err := qr.RenderToTerminal(pairURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  QR generation failed: %v\n", err)
	} else {
		fmt.Print(qrStr)
	}

	fmt.Printf("  \033[2mRemote:\033[0m %s\n", pairURL)
	fmt.Printf("  \033[2mLocal:\033[0m  %s\n", localURL)
	fmt.Println()
	fmt.Println("  \033[2mWaiting for phone... (Ctrl+C to quit)\033[0m")
	fmt.Println()

	// Poll for answer — keep re-uploading offer for reconnects
	go func() {
		for {
			pollCtx, pollCancel := context.WithTimeout(ctx, 5*time.Minute)
			answer, err := relay.PollAnswer(pollCtx, relayURL, roomID)
			pollCancel()

			if ctx.Err() != nil {
				return // main context cancelled
			}
			if err != nil {
				continue
			}
			pm.acceptAnswer(answer)

			// Re-upload a fresh offer for next reconnect
			time.Sleep(2 * time.Second)
			newOffer, err := pm.newOffer()
			if err != nil {
				continue
			}
			relay.PostOffer(relayURL, roomID, newOffer)
		}
	}()

	return nil
}

func printLocalQR(localURL string) {
	fmt.Println("  \033[33mScan QR with your phone to connect:\033[0m")
	fmt.Println("  \033[2m(phone must be on same network)\033[0m")
	fmt.Println()

	qrStr, err := qr.RenderToTerminal(localURL)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  QR generation failed: %v\n", err)
	} else {
		fmt.Print(qrStr)
	}

	fmt.Printf("  \033[2mOr open:\033[0m %s\n", localURL)
	fmt.Println()
	fmt.Println("  \033[2mWaiting for phone... (Ctrl+C to quit)\033[0m")
	fmt.Println()
}

func startHTTPServer(host string, port int, pm *peerManager) *http.Server {
	mux := http.NewServeMux()

	// Serve PWA files
	if WebFS != nil {
		mux.Handle("/", http.FileServer(http.FS(WebFS)))
	}

	// Offer endpoint — creates a fresh peer+offer each time
	mux.HandleFunc("/offer", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Access-Control-Allow-Origin", "*")

		offer, err := pm.newOffer()
		if err != nil {
			w.WriteHeader(http.StatusInternalServerError)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}
		json.NewEncoder(w).Encode(map[string]string{"offer": offer})
	})

	// Answer endpoint
	mux.HandleFunc("/answer", func(w http.ResponseWriter, r *http.Request) {
		if r.Method == "OPTIONS" {
			w.Header().Set("Access-Control-Allow-Origin", "*")
			w.Header().Set("Access-Control-Allow-Methods", "POST")
			w.Header().Set("Access-Control-Allow-Headers", "Content-Type")
			return
		}
		if r.Method != "POST" {
			http.Error(w, "POST only", http.StatusMethodNotAllowed)
			return
		}

		w.Header().Set("Access-Control-Allow-Origin", "*")

		body, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "read error", http.StatusBadRequest)
			return
		}

		var payload struct {
			Answer string `json:"answer"`
		}
		if err := json.Unmarshal(body, &payload); err != nil {
			http.Error(w, "invalid JSON", http.StatusBadRequest)
			return
		}

		if err := pm.acceptAnswer(payload.Answer); err != nil {
			w.WriteHeader(http.StatusBadRequest)
			json.NewEncoder(w).Encode(map[string]string{"error": err.Error()})
			return
		}

		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"ok":true}`))
	})

	srv := &http.Server{
		Addr:    fmt.Sprintf(":%d", port),
		Handler: mux,
	}

	go srv.ListenAndServe()
	return srv
}

func getLocalIP() string {
	addrs, err := net.InterfaceAddrs()
	if err != nil {
		return "localhost"
	}
	for _, addr := range addrs {
		if ipnet, ok := addr.(*net.IPNet); ok && !ipnet.IP.IsLoopback() && ipnet.IP.To4() != nil {
			return ipnet.IP.String()
		}
	}
	return "localhost"
}
