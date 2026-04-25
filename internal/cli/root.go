package cli

import (
	"os"

	"github.com/spf13/cobra"
)

var rootCmd = &cobra.Command{
	Use:   "deep-proxy",
	Short: "OpenAI-compatible proxy to DeepSeek's web backend",
	Long: `deep-proxy intercepts OpenAI Chat Completions requests and translates
them on-the-fly to DeepSeek's web API using your browser session token.

Point any OpenAI-compatible client at http://localhost:PORT and it will
transparently consume DeepSeek without needing an official API key.`,
	CompletionOptions: cobra.CompletionOptions{DisableDefaultCmd: true},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		os.Exit(1)
	}
}

func init() {
	rootCmd.AddCommand(startCmd, loginCmd, versionCmd)
}
