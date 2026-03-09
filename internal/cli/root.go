package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	port     int
	verbose  bool
	relayURL string

	// Set via ldflags at build time.
	Version = "dev"
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "poopilot",
		Short: "Control AI CLI agents from your phone",
		Long: `💩 poopilot — control AI coding agents from your phone.

Wraps any CLI tool (Claude, Codex, Aider, etc.) in a PTY and lets you
monitor terminal output and approve actions from your phone via P2P WebRTC.

No accounts. Just scan a QR code.`,
	}

	root.PersistentFlags().IntVarP(&port, "port", "p", 9876, "HTTP port for pairing")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")
	root.PersistentFlags().StringVar(&relayURL, "relay", "", "relay server URL for cross-network pairing (e.g. https://poopilot-relay.workers.dev)")

	root.AddCommand(newRunCmd())
	root.AddCommand(newVersionCmd())

	return root
}

func Execute() {
	if err := NewRootCmd().Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func newVersionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print version",
		Run: func(cmd *cobra.Command, args []string) {
			fmt.Printf("poopilot %s\n", Version)
		},
	}
}
