package auth

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/chromedp/cdproto/fetch"
	"github.com/chromedp/cdproto/network"
	"github.com/chromedp/chromedp"
)

const (
	deepseekURL    = "https://chat.deepseek.com"
	deepseekAPIURL = "/api/v0/"
)

// captureCredentials launches Chrome with the given allocator options, navigates
// to chat.deepseek.com, and waits for a /api/v0/* request that carries
// x-hif-leim. The Fetch domain is used (not Network events) because some
// header injection paths in DeepSeek's frontend are invisible to the
// Network.requestWillBeSent[ExtraInfo] events but always visible at the
// Fetch.requestPaused stage (which is closer to the actual wire).
//
// On capture, returns the Cookie header (built from the browser's cookie jar
// for chat.deepseek.com) and the captured x-hif-leim value.
func captureCredentials(
	parentCtx context.Context,
	opts []chromedp.ExecAllocatorOption,
	maxWait time.Duration,
	onReady func(ctx context.Context),
) (token, cookie, hifLeim string, err error) {
	allocCtx, cancelAlloc := chromedp.NewExecAllocator(parentCtx, opts...)
	defer cancelAlloc()

	chromeCtx, cancelChrome := chromedp.NewContext(allocCtx)
	defer cancelChrome()

	timeoutCtx, cancelT := context.WithTimeout(chromeCtx, maxWait)
	defer cancelT()

	var (
		mu            sync.Mutex
		capturedHif   string
		capturedToken string
	)
	captured := make(chan struct{}, 1)

	chromedp.ListenTarget(chromeCtx, func(ev interface{}) {
		e, ok := ev.(*fetch.EventRequestPaused)
		if !ok {
			return
		}
		// ALWAYS continue the request — otherwise the page hangs.
		go func() {
			_ = chromedp.Run(chromeCtx, fetch.ContinueRequest(e.RequestID))
		}()

		var hif, tok string
		for k, v := range e.Request.Headers {
			s, _ := v.(string)
			if s == "" {
				continue
			}
			switch strings.ToLower(k) {
			case "x-hif-leim":
				hif = s
			case "authorization":
				tok = strings.TrimPrefix(strings.TrimPrefix(s, "Bearer "), "bearer ")
			}
		}
		if hif == "" {
			return
		}
		mu.Lock()
		if capturedHif == "" {
			capturedHif = hif
			capturedToken = tok
			select {
			case captured <- struct{}{}:
			default:
			}
		}
		mu.Unlock()
	})

	if err := chromedp.Run(chromeCtx,
		fetch.Enable().WithPatterns([]*fetch.RequestPattern{
			{URLPattern: "*://chat.deepseek.com/api/v0/*"},
		}),
		chromedp.Navigate(deepseekURL),
	); err != nil {
		return "", "", "", fmt.Errorf("chrome navigate: %w (is Chrome installed and not blocked by another deep-proxy instance?)", err)
	}

	if onReady != nil {
		go onReady(chromeCtx)
	}

	select {
	case <-captured:
		select {
		case <-time.After(2 * time.Second):
		case <-timeoutCtx.Done():
		}
	case <-timeoutCtx.Done():
		return "", "", "", fmt.Errorf("did not see any /api/v0/ request with x-hif-leim: %w", timeoutCtx.Err())
	}

	cookieStr, err := readCookies(chromeCtx)
	if err != nil {
		return "", "", "", err
	}
	mu.Lock()
	hif := capturedHif
	tok := capturedToken
	mu.Unlock()

	if cookieStr == "" || hif == "" {
		return "", "", "", fmt.Errorf("captured empty values (cookie_len=%d hif_leim_len=%d)",
			len(cookieStr), len(hif))
	}
	return tok, cookieStr, hif, nil
}

// ChromeRefreshFunc returns a RefreshFunc that periodically launches headless
// Chrome with the given user-data dir and extracts fresh credentials.
func ChromeRefreshFunc(profileDir string) RefreshFunc {
	return func(ctx context.Context) (string, string, string, error) {
		UnlockProfile(profileDir)

		opts := append([]chromedp.ExecAllocatorOption{},
			chromedp.DefaultExecAllocatorOptions[:]...)
		opts = append(opts,
			chromedp.UserDataDir(profileDir),
			chromedp.Flag("headless", "new"),
			chromedp.Flag("disable-gpu", true),
			chromedp.Flag("no-first-run", true),
			chromedp.Flag("no-default-browser-check", true),
			chromedp.Flag("disable-blink-features", "AutomationControlled"),
		)

		// Force a chat_session/create POST so the page's interceptors generate
		// an x-hif-leim header without user interaction.
		injectFetch := func(ctx context.Context) {
			select {
			case <-time.After(3 * time.Second):
			case <-ctx.Done():
				return
			}
			_ = chromedp.Run(ctx, chromedp.Evaluate(`
				fetch('/api/v0/chat_session/create', {
					method: 'POST',
					headers: {'Content-Type': 'application/json'},
					body: JSON.stringify({character_id: null}),
					credentials: 'include'
				}).catch(() => {});
				null;
			`, nil))
		}

		return captureCredentials(ctx, opts, 90*time.Second, injectFetch)
	}
}

// VisibleLogin opens a non-headless Chrome window and waits for the user to
// sign in and produce an x-hif-leim-bearing request (e.g. by clicking
// "New Chat" or sending any test message). Returns Token, Cookie, HifLeim.
func VisibleLogin(ctx context.Context, profileDir string, maxWait time.Duration) (token, cookie, hifLeim string, err error) {
	UnlockProfile(profileDir)

	opts := append([]chromedp.ExecAllocatorOption{},
		chromedp.DefaultExecAllocatorOptions[:]...)
	opts = append(opts,
		chromedp.UserDataDir(profileDir),
		chromedp.Flag("headless", false),
		chromedp.Flag("no-first-run", true),
		chromedp.Flag("no-default-browser-check", true),
	)

	return captureCredentials(ctx, opts, maxWait, nil)
}

// readCookies fetches all cookies that would be sent to chat.deepseek.com and
// formats them as a Cookie header value (`name1=value1; name2=value2`).
func readCookies(ctx context.Context) (string, error) {
	rctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	var cookies []*network.Cookie
	err := chromedp.Run(rctx, chromedp.ActionFunc(func(c context.Context) error {
		var inner error
		cookies, inner = network.GetCookies().WithURLs([]string{deepseekURL}).Do(c)
		return inner
	}))
	if err != nil {
		return "", fmt.Errorf("get cookies: %w", err)
	}

	var sb strings.Builder
	for i, c := range cookies {
		if i > 0 {
			sb.WriteString("; ")
		}
		sb.WriteString(c.Name)
		sb.WriteByte('=')
		sb.WriteString(c.Value)
	}
	return sb.String(), nil
}
