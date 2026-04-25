package upstream

import "net/http"

// Default static headers — values mirror the DeepSeek web client at the time of
// capture (2026-04-25). Update via flags if DeepSeek ships a new web build.
const (
	defaultUA            = "Mozilla/5.0 (iPhone; CPU iPhone OS 18_5 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.5 Mobile/15E148 Safari/604.1"
	defaultAppVersion    = "20241129.1"
	defaultClientVersion = "1.8.0"
	defaultPlatform      = "web"
	defaultLocale        = "en"
	defaultTimezone      = "0"
)

// HeaderConfig is the per-session set of values that the DeepSeek web client
// computes dynamically (Cloudflare cookies, proof-of-work, anti-bot signature).
// They expire periodically — refresh from your browser DevTools when requests
// start failing with code 40300 (MISSING_HEADER).
type HeaderConfig struct {
	Token          string
	UserAgent      string
	AppVersion     string
	ClientVersion  string
	ClientPlatform string
	ClientLocale   string
	TimezoneOffset string
	Cookie         string
	PowResponse    string
	HifLeim        string
}

func (h HeaderConfig) withDefaults() HeaderConfig {
	if h.UserAgent == "" {
		h.UserAgent = defaultUA
	}
	if h.AppVersion == "" {
		h.AppVersion = defaultAppVersion
	}
	if h.ClientVersion == "" {
		h.ClientVersion = defaultClientVersion
	}
	if h.ClientPlatform == "" {
		h.ClientPlatform = defaultPlatform
	}
	if h.ClientLocale == "" {
		h.ClientLocale = defaultLocale
	}
	if h.TimezoneOffset == "" {
		h.TimezoneOffset = defaultTimezone
	}
	return h
}

func setBrowserHeaders(req *http.Request, h HeaderConfig) {
	h = h.withDefaults()

	req.Header.Set("Authorization", "Bearer "+h.Token)
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "*/*")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	req.Header.Set("User-Agent", h.UserAgent)
	req.Header.Set("Origin", "https://chat.deepseek.com")
	req.Header.Set("Referer", "https://chat.deepseek.com/")

	// Static x-client-* headers DeepSeek validates on every call.
	req.Header.Set("x-app-version", h.AppVersion)
	req.Header.Set("x-client-version", h.ClientVersion)
	req.Header.Set("x-client-platform", h.ClientPlatform)
	req.Header.Set("x-client-locale", h.ClientLocale)
	req.Header.Set("x-client-timezone-offset", h.TimezoneOffset)

	// Dynamic per-session values. Without these DeepSeek returns
	// {"code":40300,"msg":"MISSING_HEADER"}.
	if h.Cookie != "" {
		req.Header.Set("Cookie", h.Cookie)
	}
	if h.PowResponse != "" {
		req.Header.Set("x-ds-pow-response", h.PowResponse)
	}
	if h.HifLeim != "" {
		req.Header.Set("x-hif-leim", h.HifLeim)
	}

	// sec-ch-* / sec-fetch-* round out the browser fingerprint.
	req.Header.Set("sec-ch-ua", `"Chromium";v="124", "Not.A/Brand";v="99"`)
	req.Header.Set("sec-ch-ua-mobile", "?0")
	req.Header.Set("sec-ch-ua-platform", `"iOS"`)
	req.Header.Set("sec-fetch-dest", "empty")
	req.Header.Set("sec-fetch-mode", "cors")
	req.Header.Set("sec-fetch-site", "same-origin")
}
