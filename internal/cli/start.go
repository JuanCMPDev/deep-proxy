package cli

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/auth"
	"github.com/JuanCMPDev/deep-proxy/internal/config"
	"github.com/JuanCMPDev/deep-proxy/internal/observability"
	"github.com/JuanCMPDev/deep-proxy/internal/proxy"
	"github.com/spf13/cobra"
)

const refreshInterval = 25 * time.Minute

var startCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the OpenAI-compatible proxy server",
	RunE:  runStart,
}

func init() {
	f := startCmd.Flags()
	f.String("token", "", "DeepSeek web session token (or set DEEPPROXY_TOKEN)")
	f.Int("port", config.Default.Port, "Port to listen on")
	f.String("host", config.Default.Host, "Host to bind to")
	f.String("upstream-base", config.Default.UpstreamBase, "DeepSeek base URL")
	f.Duration("timeout", config.Default.Timeout, "Per-request timeout")
	f.String("log-level", config.Default.LogLevel, "Log level: debug|info|warn|error")
	f.String("log-format", config.Default.LogFormat, "Log format: text|json")
	f.String("model", config.Default.Model, "Default model (deepseek-chat, deepseek-reasoner)")
	f.Bool("thinking", config.Default.ThinkingEnabled, "Enable DeepSeek extended thinking")
	f.Bool("search", config.Default.SearchEnabled, "Enable DeepSeek web search")

	// Per-session anti-bot values (or set DEEPPROXY_COOKIE / _POW_RESPONSE / _HIF_LEIM).
	f.String("cookie", "", "Full Cookie header from your browser session")
	f.String("pow-response", "", "x-ds-pow-response header from your browser session")
	f.String("hif-leim", "", "x-hif-leim header from your browser session")

	// Auto-refresh via headless Chrome (requires `deep-proxy login` first).
	f.Bool("auto-refresh", false, "Auto-refresh Cookie + x-hif-leim every 25 min via headless Chrome")

	// Optional fingerprint overrides.
	f.String("user-agent", "", "Override User-Agent (defaults to mobile Safari)")
	f.String("app-version", "", "Override x-app-version (defaults to 20241129.1)")
	f.String("client-version", "", "Override x-client-version (defaults to 1.8.0)")
	f.String("client-locale", "", "Override x-client-locale (defaults to en)")
	f.String("client-timezone", "", "Override x-client-timezone-offset (defaults to 0)")
}

func runStart(cmd *cobra.Command, _ []string) error {
	f := cmd.Flags()

	token := stringWithEnv(f, "token", "DEEPPROXY_TOKEN")
	// Fall back to credentials file if token wasn't supplied.
	if token == "" {
		if creds, err := auth.ReadCredentials(); err == nil && creds != nil && creds.Token != "" {
			token = creds.Token
		}
	}
	if token == "" {
		return fmt.Errorf("--token flag, DEEPPROXY_TOKEN env var, or `deep-proxy login` is required")
	}

	host, _ := f.GetString("host")
	if host == "0.0.0.0" {
		fmt.Fprintln(os.Stderr, "WARNING: binding to 0.0.0.0 exposes the proxy on all network interfaces")
	}

	port, _ := f.GetInt("port")
	upstreamBase, _ := f.GetString("upstream-base")
	timeout, _ := f.GetDuration("timeout")
	logLevel, _ := f.GetString("log-level")
	logFormat, _ := f.GetString("log-format")
	model, _ := f.GetString("model")
	thinking, _ := f.GetBool("thinking")
	search, _ := f.GetBool("search")
	autoRefresh, _ := f.GetBool("auto-refresh")

	cookie := stringWithEnv(f, "cookie", "DEEPPROXY_COOKIE")
	powResponse := stringWithEnv(f, "pow-response", "DEEPPROXY_POW_RESPONSE")
	hifLeim := stringWithEnv(f, "hif-leim", "DEEPPROXY_HIF_LEIM")

	// If neither flag nor env var supplied them, fall back to the credentials
	// cache written by `deep-proxy login`.
	if cookie == "" || hifLeim == "" {
		if creds, err := auth.ReadCredentials(); err == nil && creds != nil {
			if cookie == "" {
				cookie = creds.Cookie
			}
			if hifLeim == "" {
				hifLeim = creds.HifLeim
			}
		}
	}

	userAgent, _ := f.GetString("user-agent")
	appVersion, _ := f.GetString("app-version")
	clientVersion, _ := f.GetString("client-version")
	clientLocale, _ := f.GetString("client-locale")
	clientTimezone, _ := f.GetString("client-timezone")

	cfg := &config.Config{
		Token:           token,
		Port:            port,
		Host:            host,
		UpstreamBase:    upstreamBase,
		Timeout:         timeout,
		LogLevel:        logLevel,
		LogFormat:       logFormat,
		Model:           model,
		ThinkingEnabled: thinking,
		SearchEnabled:   search,
		Cookie:          cookie,
		PowResponse:     powResponse,
		HifLeim:         hifLeim,
		AutoRefresh:     autoRefresh,
		UserAgent:       userAgent,
		AppVersion:      appVersion,
		ClientVersion:   clientVersion,
		ClientLocale:    clientLocale,
		TimezoneOffset:  clientTimezone,
	}

	log := observability.NewLogger(logLevel, logFormat)
	slog.SetDefault(log)

	log.Info("deep-proxy starting",
		slog.String("addr", cfg.Addr()),
		slog.String("upstream", cfg.UpstreamBase),
		slog.String("model", cfg.Model),
		slog.Bool("thinking", cfg.ThinkingEnabled),
		slog.Bool("search", cfg.SearchEnabled),
		slog.Bool("cookie_set", cfg.Cookie != ""),
		slog.Bool("hif_leim_set", cfg.HifLeim != ""),
		slog.Bool("auto_refresh", cfg.AutoRefresh),
	)

	if creds, err := auth.ReadCredentials(); err == nil && creds != nil {
		age := time.Since(creds.SavedAtTime())
		log.Info("credentials cache loaded",
			slog.Duration("age", age.Round(time.Second)),
			slog.String("hint", "rerun `deep-proxy login` if requests start failing"),
		)
	} else if cfg.Cookie == "" || cfg.HifLeim == "" {
		log.Warn("no credentials available — every request will fail",
			slog.String("hint", "run `deep-proxy login` first, or set DEEPPROXY_COOKIE / DEEPPROXY_HIF_LEIM env vars"),
		)
	}

	ctx, cancel := signal.NotifyContext(context.Background(), os.Interrupt)
	defer cancel()

	store := auth.NewHeaderStore(cfg.Token, cfg.Cookie, cfg.HifLeim)

	if cfg.AutoRefresh {
		profileDir, err := auth.ProfileDir()
		if err != nil {
			return fmt.Errorf("resolve chrome profile dir: %w", err)
		}
		switch {
		case !auth.ProfileExists(profileDir):
			log.Warn("auto-refresh skipped: no Chrome profile found",
				slog.String("profile_dir", profileDir),
				slog.String("hint", "run `deep-proxy login` first to create the profile"),
			)
		default:
			log.Info("running initial credential refresh (this may take up to 90s)",
				slog.String("profile_dir", profileDir),
			)
			refresher := auth.NewRefresher(store, auth.ChromeRefreshFunc(profileDir), refreshInterval)
			if err := refresher.Start(ctx); err != nil {
				log.Warn("auto-refresh disabled — falling back to static credentials",
					slog.String("error", err.Error()),
				)
			} else {
				log.Info("auto-refresh enabled",
					slog.String("profile_dir", profileDir),
					slog.Duration("interval", refreshInterval),
				)
				defer refresher.Stop()
			}
		}
	}

	srv, err := proxy.NewServer(cfg, store, log)
	if err != nil {
		return fmt.Errorf("init server: %w", err)
	}
	log.Info("ready — listening for OpenAI-compatible requests",
		slog.String("addr", "http://"+cfg.Addr()+"/v1/chat/completions"),
	)
	if err := proxy.Serve(ctx, srv, log); err != nil {
		return fmt.Errorf("server error: %w", err)
	}
	return nil
}

func stringWithEnv(f interface {
	GetString(string) (string, error)
}, flagName, envName string) string {
	v, _ := f.GetString(flagName)
	if v != "" {
		return v
	}
	return os.Getenv(envName)
}
