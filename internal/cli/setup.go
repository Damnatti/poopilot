package cli

import (
	"fmt"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/spf13/cobra"
)

func newSetupCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "setup",
		Short: "Show current config and set up cross-network relay",
		Long:  "Shows the current poopilot configuration and guides you through setting up a relay for cross-network phone pairing.",
		RunE:  runSetup,
	}
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("  💩 \033[1mpoopilot setup\033[0m " + Version)
	fmt.Println()

	// Current config
	fmt.Println("  \033[1mCurrent configuration:\033[0m")
	fmt.Println()

	localIP := getLocalIP()
	fmt.Printf("  Local IP:    %s\n", localIP)
	fmt.Printf("  Port:        %d\n", port)

	// Check local port availability
	ln, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
	if err != nil {
		fmt.Printf("  Local mode:  \033[33m⚠ port %d in use (poopilot already running?)\033[0m\n", port)
	} else {
		ln.Close()
		fmt.Printf("  Local mode:  \033[32m✓ ready\033[0m (phone must be on same network)\n")
	}

	// Relay status
	existing := os.Getenv("POOPILOT_RELAY")
	if existing != "" {
		ok := testRelay(existing)
		if ok {
			fmt.Printf("  Relay:       \033[32m✓ %s\033[0m\n", existing)
			fmt.Printf("  Remote mode: \033[32m✓ ready\033[0m (phone can be on any network)\n")
		} else {
			fmt.Printf("  Relay:       \033[31m✗ %s (unreachable)\033[0m\n", existing)
			fmt.Printf("  Remote mode: \033[31m✗ relay down — check URL or redeploy\033[0m\n")
		}
	} else {
		fmt.Printf("  Relay:       \033[2mnot configured\033[0m\n")
		fmt.Printf("  Remote mode: \033[2mdisabled\033[0m (phone must be on same network)\n")
	}

	fmt.Println()

	// If relay is configured and working, we're done
	if existing != "" && testRelay(existing) {
		fmt.Println("  \033[32mAll good!\033[0m Run: \033[1mpoopilot run claude\033[0m")
		fmt.Println()
		return nil
	}

	// Guide for relay setup
	fmt.Println("  ─────────────────────────────────────────────")
	fmt.Println()
	fmt.Println("  \033[1mSet up relay for any-network access:\033[0m")
	fmt.Println()
	fmt.Println("  The relay is a free Cloudflare Worker (~50 lines) that only")
	fmt.Println("  passes handshake metadata (~2KB). Terminal data stays P2P.")
	fmt.Println()
	fmt.Println("  \033[1m1.\033[0m Deploy the relay")
	fmt.Println()
	fmt.Println("     Browser (one-click):")
	fmt.Println("     \033[4mhttps://deploy.workers.cloudflare.com/?url=https://github.com/Damnatti/poopilot/tree/main/relay\033[0m")
	fmt.Println()
	fmt.Println("     Or CLI:")
	fmt.Println("       git clone https://github.com/Damnatti/poopilot.git")
	fmt.Println("       cd poopilot/relay")
	fmt.Println("       npx wrangler login")
	fmt.Println("       npx wrangler kv namespace create ROOMS  # paste ID into wrangler.toml")
	fmt.Println("       npx wrangler deploy")
	fmt.Println()

	shell := detectShell()
	rc := shellRC(shell)

	fmt.Println("  \033[1m2.\033[0m Save your relay URL")
	fmt.Println()
	fmt.Printf("       echo 'export POOPILOT_RELAY=https://YOUR-RELAY.workers.dev' >> %s\n", rc)
	fmt.Printf("       source %s\n", rc)
	fmt.Println()
	fmt.Println("  \033[1m3.\033[0m Verify")
	fmt.Println()
	fmt.Println("       poopilot setup")
	fmt.Println()

	return nil
}

func testRelay(url string) bool {
	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Get(strings.TrimRight(url, "/") + "/relay/healthcheck/offer")
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	// 404 with JSON body means the relay is running (room not found = expected)
	return resp.StatusCode == 404 || resp.StatusCode == 200
}

func detectShell() string {
	shell := os.Getenv("SHELL")
	if strings.Contains(shell, "zsh") {
		return "zsh"
	}
	if strings.Contains(shell, "fish") {
		return "fish"
	}
	return "bash"
}

func shellRC(shell string) string {
	home, _ := os.UserHomeDir()
	switch shell {
	case "zsh":
		return filepath.Join(home, ".zshrc")
	case "fish":
		return filepath.Join(home, ".config/fish/config.fish")
	default:
		return filepath.Join(home, ".bashrc")
	}
}
