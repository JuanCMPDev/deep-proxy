package config

import (
	"fmt"
	"time"
)

type Config struct {
	Token        string
	Port         int
	Host         string
	UpstreamBase string
	Timeout      time.Duration
	LogLevel     string
	LogFormat    string
	Model        string

	ThinkingEnabled bool
	SearchEnabled   bool

	// Per-session anti-bot values that the DeepSeek web client computes
	// dynamically. Refresh from your browser DevTools when DeepSeek starts
	// returning code 40300 (MISSING_HEADER), or enable AutoRefresh.
	Cookie      string
	PowResponse string
	HifLeim     string

	// AutoRefresh enables a background goroutine that re-extracts Cookie and
	// HifLeim from a real Chrome session every ~25 minutes. Requires that
	// `deep-proxy login` has been run at least once to seed the profile.
	AutoRefresh bool

	// Optional overrides for the static x-client-* headers and User-Agent.
	UserAgent      string
	AppVersion     string
	ClientVersion  string
	ClientPlatform string
	ClientLocale   string
	TimezoneOffset string
}

func (c *Config) Addr() string {
	return fmt.Sprintf("%s:%d", c.Host, c.Port)
}

func (c *Config) ChatURL() string {
	return c.UpstreamBase + "/api/v0/chat/completion"
}

var Default = &Config{
	Port:            3000,
	Host:            "127.0.0.1",
	UpstreamBase:    "https://chat.deepseek.com",
	Timeout:         5 * time.Minute,
	LogLevel:        "info",
	LogFormat:       "text",
	Model:           "deepseek-chat",
	ThinkingEnabled: false,
	SearchEnabled:   false,
}
