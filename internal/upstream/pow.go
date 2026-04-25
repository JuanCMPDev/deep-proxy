package upstream

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"time"
)

// powChallenge mirrors the "challenge" object returned by /api/v0/chat/create_pow_challenge.
type powChallenge struct {
	Algorithm   string `json:"algorithm"`
	Challenge   string `json:"challenge"`
	Salt        string `json:"salt"`
	Signature   string `json:"signature"`
	Difficulty  int    `json:"difficulty"`
	ExpireAt    int64  `json:"expire_at"`
	ExpireAfter int    `json:"expire_after"`
	TargetPath  string `json:"target_path"`
}

type powChallengeResponse struct {
	Code int    `json:"code"`
	Msg  string `json:"msg"`
	Data struct {
		BizCode int    `json:"biz_code"`
		BizMsg  string `json:"biz_msg"`
		BizData struct {
			Challenge powChallenge `json:"challenge"`
		} `json:"biz_data"`
	} `json:"data"`
}

// powAnswer is the JSON payload that gets base64-encoded into x-ds-pow-response.
// Field order matches the JS client exactly: algorithm, challenge, salt, answer,
// signature, target_path.
type powAnswer struct {
	Algorithm  string `json:"algorithm"`
	Challenge  string `json:"challenge"`
	Salt       string `json:"salt"`
	Answer     int    `json:"answer"`
	Signature  string `json:"signature"`
	TargetPath string `json:"target_path"`
}

// fetchChallenge POSTs to /api/v0/chat/create_pow_challenge and returns the parsed challenge.
func (c *Client) fetchChallenge(ctx context.Context, targetPath string) (*powChallenge, error) {
	body, err := json.Marshal(map[string]string{"target_path": targetPath})
	if err != nil {
		return nil, err
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.challengeURL, bytes.NewReader(body))
	if err != nil {
		return nil, err
	}

	// Challenge endpoint itself does not require a PoW header.
	h := c.currentHeaders()
	h.PowResponse = ""
	setBrowserHeaders(req, h)

	resp, err := c.http.Do(req)
	if err != nil {
		return nil, fmt.Errorf("fetch challenge: %w", err)
	}
	defer resp.Body.Close()

	raw, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("challenge endpoint status %d: %s", resp.StatusCode, string(raw))
	}

	var pr powChallengeResponse
	if err := json.Unmarshal(raw, &pr); err != nil {
		return nil, fmt.Errorf("decode challenge response: %w (body=%s)", err, string(raw))
	}
	if pr.Code != 0 {
		return nil, fmt.Errorf("challenge api error code=%d msg=%q", pr.Code, pr.Msg)
	}
	return &pr.Data.BizData.Challenge, nil
}

// computePoWHeader fetches a fresh challenge, runs the WASM solver, and returns
// the base64-encoded JSON answer payload that goes into x-ds-pow-response.
func (c *Client) computePoWHeader(ctx context.Context, targetPath string) (string, error) {
	start := time.Now()

	ch, err := c.fetchChallenge(ctx, targetPath)
	if err != nil {
		return "", err
	}
	slog.Debug("pow challenge fetched",
		slog.String("salt", ch.Salt),
		slog.Int("difficulty", ch.Difficulty),
		slog.Int64("expire_at", ch.ExpireAt),
		slog.Duration("fetch_ms", time.Since(start)),
	)

	solveStart := time.Now()
	answer, err := c.solver.Solve(ctx, ch.Challenge, ch.Salt, ch.Difficulty, ch.ExpireAt)
	if err != nil {
		return "", fmt.Errorf("solve pow: %w", err)
	}
	slog.Debug("pow solved",
		slog.Int("answer", answer),
		slog.Duration("solve_ms", time.Since(solveStart)),
	)

	raw, err := json.Marshal(powAnswer{
		Algorithm:  ch.Algorithm,
		Challenge:  ch.Challenge,
		Salt:       ch.Salt,
		Answer:     answer,
		Signature:  ch.Signature,
		TargetPath: ch.TargetPath,
	})
	if err != nil {
		return "", err
	}
	return base64.StdEncoding.EncodeToString(raw), nil
}
