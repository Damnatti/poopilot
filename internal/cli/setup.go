package cli

import (
	"fmt"
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
		Short: "Configure poopilot for cross-network access",
		Long:  "Guides you through setting up a relay for cross-network phone pairing.",
		RunE:  runSetup,
	}
}

func runSetup(cmd *cobra.Command, args []string) error {
	fmt.Println()
	fmt.Println("  💩 \033[1mpoopilot setup\033[0m")
	fmt.Println()

	// Check if relay is already configured
	existing := os.Getenv("POOPILOT_RELAY")
	if existing != "" {
		fmt.Printf("  \033[32m✓\033[0m Relay already configured: %s\n", existing)
		ok := testRelay(existing)
		if ok {
			fmt.Println("  \033[32m✓\033[0m Relay is reachable")
			fmt.Println()
			fmt.Println("  You're all set! Run: \033[1mpoopilot run claude\033[0m")
			fmt.Println()
			return nil
		}
		fmt.Println("  \033[31m✗\033[0m Relay is not reachable — check the URL or redeploy")
		fmt.Println()
	}

	// Guide the user
	fmt.Println("  To use poopilot from any network, you need a signaling relay.")
	fmt.Println("  It's a free Cloudflare Worker that only passes connection")
	fmt.Println("  metadata (~2KB). Your terminal data never touches it.")
	fmt.Println()
	fmt.Println("  \033[1mStep 1:\033[0m Deploy the relay")
	fmt.Println()
	fmt.Println("  Option A — One-click (browser):")
	fmt.Println("  Open: \033[4mhttps://deploy.workers.cloudflare.com/?url=https://github.com/Damnatti/poopilot/tree/main/relay\033[0m")
	fmt.Println()
	fmt.Println("  Option B — CLI:")
	fmt.Println("    cd relay")
	fmt.Println("    npx wrangler login")
	fmt.Println("    npx wrangler kv namespace create ROOMS  # paste ID into wrangler.toml")
	fmt.Println("    npx wrangler deploy")
	fmt.Println()
	fmt.Println("  \033[1mStep 2:\033[0m Add your relay URL to your shell profile")
	fmt.Println()

	shell := detectShell()
	rc := shellRC(shell)

	fmt.Printf("    echo 'export POOPILOT_RELAY=https://YOUR-RELAY.workers.dev' >> %s\n", rc)
	fmt.Printf("    source %s\n", rc)
	fmt.Println()
	fmt.Println("  \033[1mStep 3:\033[0m Verify")
	fmt.Println()
	fmt.Println("    poopilot setup")
	fmt.Println()
	fmt.Println("  After that, \033[1mpoopilot run claude\033[0m works from any network.")
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
