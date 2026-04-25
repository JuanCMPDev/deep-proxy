package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/auth"
	"github.com/spf13/cobra"
)

var loginCmd = &cobra.Command{
	Use:   "login",
	Short: "One-time setup: log into chat.deepseek.com via Chrome",
	Long: `Opens a Chrome window pointed at chat.deepseek.com using a dedicated
profile that deep-proxy will reuse for automatic credential refreshes.

Sign in normally; the command waits for an authenticated /api/v0/ request and
exits as soon as it sees one. Two artifacts are saved:

  - Chrome profile     ~/.config/deep-proxy/chrome-profile/   (or platform equivalent)
  - Credentials cache  ~/.config/deep-proxy/credentials.json  (Cookie + x-hif-leim)

The credentials cache is what ` + "`deep-proxy start`" + ` reads when no env vars
are set. Re-run this command every ~30 minutes (when DeepSeek starts returning
errors) to refresh the credentials.`,
	RunE: runLogin,
}

func init() {
	loginCmd.Flags().Duration("max-wait", 15*time.Minute, "How long to wait for the user to complete login")
}

func runLogin(cmd *cobra.Command, _ []string) error {
	maxWait, _ := cmd.Flags().GetDuration("max-wait")

	profileDir, err := auth.ProfileDir()
	if err != nil {
		return err
	}
	if err := auth.EnsureProfileDir(profileDir); err != nil {
		return fmt.Errorf("create profile dir: %w", err)
	}

	credPath, _ := auth.CredFile()
	fmt.Fprintf(os.Stderr, "Opening Chrome with profile %s\n", profileDir)
	fmt.Fprintln(os.Stderr, "Steps:")
	fmt.Fprintln(os.Stderr, "  1. Sign in to chat.deepseek.com (handles any Cloudflare/captcha)")
	fmt.Fprintln(os.Stderr, "  2. Click 'New chat' OR send any test message — this triggers the security signature")
	fmt.Fprintln(os.Stderr, "deep-proxy will close the window automatically once captured.")

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	token, cookie, hifLeim, err := auth.VisibleLogin(ctx, profileDir, maxWait)
	if err != nil {
		return fmt.Errorf("login: %w", err)
	}

	if err := auth.WriteCredentials(&auth.Credentials{
		Token:   token,
		Cookie:  cookie,
		HifLeim: hifLeim,
		SavedAt: time.Now().Unix(),
	}); err != nil {
		fmt.Fprintf(os.Stderr, "warning: could not save credentials to %s: %v\n", credPath, err)
	}

	fmt.Fprintln(os.Stderr, "✓ Logged in.")
	fmt.Fprintf(os.Stderr, "  token captured  : %v\n", token != "")
	fmt.Fprintf(os.Stderr, "  cookie captured : %v (%d bytes)\n", cookie != "", len(cookie))
	fmt.Fprintf(os.Stderr, "  x-hif-leim      : %v (%d bytes)\n", hifLeim != "", len(hifLeim))
	fmt.Fprintf(os.Stderr, "✓ Credentials cached at %s (valid ~30 minutes).\n", credPath)
	fmt.Fprintln(os.Stderr, "Run `deep-proxy start` to use them.")
	return nil
}
