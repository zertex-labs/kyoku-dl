package cmd

import (
	"fmt"

	"github.com/spf13/cobra"
)

// These variables are set at build time via ldflags.
// Example: go build -ldflags "-X github.com/zertex-labs/kyoku-dl/cmd.Version=1.0.0 -X github.com/zertex-labs/kyoku-dl/cmd.Commit=abc1234 -X github.com/zertex-labs/kyoku-dl/cmd.BuildDate=2025-01-01"
var (
	Version   = "dev"
	Commit    = "none"
	BuildDate = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Printf("kyoku-dl %s\n", Version)
		fmt.Printf("  commit:  %s\n", Commit)
		fmt.Printf("  built:   %s\n", BuildDate)
	},
}
