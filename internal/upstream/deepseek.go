package upstream

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/auth"
)

const (
	chatPath      = "/api/v0/chat/completion"
	challengePath = "/api/v0/chat/create_pow_challenge"
	sessionPath   = "/api/v0/chat_session/create"
)

// Request is the exact payload shape that chat.deepseek.com/api/v0/chat/completion expects.
type Request struct {
	ChatSessionID   string   `json:"chat_session_id"`
	ParentMessageID *string  `json:"parent_message_id"`
	ModelType       string   `json:"model_type"`
	Prompt          string   `json:"prompt"`
	RefFileIDs      []string `json:"ref_file_ids"`
	ThinkingEnabled bool     `json:"thinking_enabled"`
	SearchEnabled   bool     `json:"search_enabled"`
	Preempt         bool     `json:"preempt"`
}

// Client sends requests to DeepSeek's web backend.
type Client struct {
	headers      HeaderConfig
	store        *auth.HeaderStore // optional; when set, overrides Cookie/HifLeim
	chatURL      string
	challengeURL string
	sessionURL   string
	http         *http.Client
	solver       *powSolver
	// powSupplier computes the x-ds-pow-response header for a target path.
	// Defaults to computePoWHeader; overridable in tests to avoid WASM execution.
	powSupplier func(ctx context.Context, targetPath string) (string, error)
}

// NewClient builds a Client. If store is non-nil, the Cookie and HifLeim
// values from HeaderConfig are treated as initial seeds; per-request the
// Client reads the latest values from store, allowing a background
// Refresher to update credentials without a Client rebuild.
func NewClient(h HeaderConfig, store *auth.HeaderStore, baseURL string, timeout time.Duration) (*Client, error) {
	solver, err := newPoWSolver(context.Background())
	if err != nil {
		return nil, fmt.Errorf("init pow solver: %w", err)
	}
	c := &Client{
		headers:      h,
		store:        store,
		chatURL:      baseURL + chatPath,
		challengeURL: baseURL + challengePath,
		sessionURL:   baseURL + sessionPath,
		http: &http.Client{
			Transport: newTransport(),
			Timeout:   timeout,
		},
		solver: solver,
	}
	c.powSupplier = c.computePoWHeader
	return c, nil
}

// currentHeaders returns a HeaderConfig with the latest Token/Cookie/HifLeim
// values pulled from the dynamic store (if configured) overlaid on the static fields.
func (c *Client) currentHeaders() HeaderConfig {
	h := c.headers
	if c.store != nil {
		token, cookie, hifLeim := c.store.Get()
		if token != "" {
			h.Token = token
		}
		if cookie != "" {
			h.Cookie = cookie
		}
		if hifLeim != "" {
			h.HifLeim = hifLeim
		}
	}
	return h
}

// Close releases the embedded WASM runtime.
func (c *Client) Close(ctx context.Context) error {
	return c.solver.Close(ctx)
}

// Do sends a chat completion request to DeepSeek.
// It creates a fresh chat session, solves the PoW challenge automatically,
// and retries once if DeepSeek rejects the PoW (code 40301).
func (c *Client) Do(ctx context.Context, req *Request) (*http.Response, error) {
	if req.ChatSessionID == "" {
		sid, err := c.createChatSession(ctx)
		if err != nil {
			return nil, fmt.Errorf("create chat session: %w", err)
		}
		req.ChatSessionID = sid
	}

	const maxAttempts = 2
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		// On retry, bypass any manually-provided PoW so a fresh challenge is
		// fetched — the previous one was already consumed and rejected.
		forceFresh := attempt > 1
		resp, retryable, err := c.sendChat(ctx, req, forceFresh)
		if err != nil {
			return nil, err
		}
		if !retryable {
			return resp, nil
		}
		slog.Debug("pow rejected, retrying with fresh challenge",
			slog.Int("attempt", attempt),
		)
	}
	return nil, fmt.Errorf("upstream rejected PoW after %d attempts", maxAttempts)
}

// sendChat issues one HTTP request to the chat endpoint.
// Returns (response, retryable, error).
// retryable is true when DeepSeek returned code 40301 (INVALID_POW_RESPONSE);
// in that case the response body has already been consumed and closed.
func (c *Client) sendChat(ctx context.Context, req *Request, forceFreshPoW bool) (*http.Response, bool, error) {
	body, err := json.Marshal(req)
	if err != nil {
		return nil, false, fmt.Errorf("marshal request: %w", err)
	}

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodPost, c.chatURL, bytes.NewReader(body))
	if err != nil {
		return nil, false, fmt.Errorf("create request: %w", err)
	}

	h := c.currentHeaders()
	if h.PowResponse == "" || forceFreshPoW {
		pow, err := c.powSupplier(ctx, chatPath)
		if err != nil {
			return nil, false, fmt.Errorf("compute pow: %w", err)
		}
		h.PowResponse = pow
	}
	setBrowserHeaders(httpReq, h)

	resp, err := c.http.Do(httpReq)
	if err != nil {
		return nil, false, fmt.Errorf("upstream request: %w", err)
	}

	// text/event-stream is the normal success path — pass body through untouched.
	if strings.HasPrefix(resp.Header.Get("Content-Type"), "text/event-stream") {
		return resp, false, nil
	}

	// JSON response means an error payload — buffer, decode, decide.
	raw, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	if readErr != nil {
		return nil, false, fmt.Errorf("read error body: %w", readErr)
	}

	var ue struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
	}
	if jsonErr := json.Unmarshal(raw, &ue); jsonErr != nil {
		// Unrecognised body — surface with original status so the handler can react.
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, false, nil
	}

	switch ue.Code {
	case 40301:
		// INVALID_POW_RESPONSE — single retry with a freshly-computed challenge.
		slog.Warn("upstream rejected PoW", slog.String("msg", ue.Msg))
		return nil, true, nil
	case 40300:
		// MISSING_HEADER — configuration error, no point retrying.
		return nil, false, fmt.Errorf("upstream rejected request (code 40300 MISSING_HEADER): %s — check --cookie and --hif-leim", ue.Msg)
	default:
		resp.Body = io.NopCloser(bytes.NewReader(raw))
		return resp, false, nil
	}
}

// createChatSession requests a fresh session ID from /chat_session/create.
func (c *Client) createChatSession(ctx context.Context) (string, error) {
	body := []byte(`{"character_id":null}`)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.sessionURL, bytes.NewReader(body))
	if err != nil {
		return "", err
	}

	h := c.currentHeaders()
	h.PowResponse = ""
	setBrowserHeaders(req, h)

	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", err
	}
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("create_session status %d: %s", resp.StatusCode, string(raw))
	}

	var r struct {
		Code int    `json:"code"`
		Msg  string `json:"msg"`
		Data struct {
			BizCode int    `json:"biz_code"`
			BizMsg  string `json:"biz_msg"`
			BizData struct {
				ChatSession struct {
					ID string `json:"id"`
				} `json:"chat_session"`
			} `json:"biz_data"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &r); err != nil {
		return "", fmt.Errorf("decode session response: %w (body=%s)", err, string(raw))
	}
	if r.Code != 0 || r.Data.BizCode != 0 {
		return "", fmt.Errorf("create_session error code=%d biz_code=%d msg=%q", r.Code, r.Data.BizCode, r.Data.BizMsg)
	}
	if r.Data.BizData.ChatSession.ID == "" {
		return "", fmt.Errorf("create_session returned empty id, raw body: %s", string(raw))
	}
	return r.Data.BizData.ChatSession.ID, nil
}

// NewSessionID generates a random UUID v4 (kept for internal use).
func NewSessionID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	b[6] = (b[6] & 0x0f) | 0x40
	b[8] = (b[8] & 0x3f) | 0x80
	return fmt.Sprintf("%08x-%04x-%04x-%04x-%012x",
		b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}
