package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// Injected at build time via -ldflags.
var (
	version = "dev"
	commit  = "none"
	date    = "unknown"
)

var versionCmd = &cobra.Command{
	Use:   "version",
	Short: "Print version information",
	Run: func(_ *cobra.Command, _ []string) {
		fmt.Printf("deep-proxy %s (commit: %s, built: %s)\n", version, commit, date)
	},
}
