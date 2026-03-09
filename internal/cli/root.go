package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var (
	port    int
	verbose bool
)

func NewRootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "poopilot",
		Short: "Control AI CLI agents from your phone",
		Long:  "poopilot wraps CLI tools (Claude, Codex, etc.) and lets you monitor and approve actions from your phone via P2P WebRTC.",
	}

	root.PersistentFlags().IntVarP(&port, "port", "p", 9876, "HTTP port for PWA serving during pairing")
	root.PersistentFlags().BoolVarP(&verbose, "verbose", "v", false, "verbose output")

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
			fmt.Println("poopilot v0.1.0")
		},
	}
}
