package upstream

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

// mockPowSupplier replaces the WASM solver in tests with a no-op that returns
// a constant token. The test server doesn't validate the PoW value.
func mockPowSupplier(_ context.Context, _ string) (string, error) {
	return "test-pow-token", nil
}

// sessionHandler returns a minimal valid session creation response.
func sessionHandler(w http.ResponseWriter, _ *http.Request) {
	_ = json.NewEncoder(w).Encode(map[string]any{
		"code": 0, "msg": "",
		"data": map[string]any{
			"biz_code": 0, "biz_msg": "",
			"biz_data": map[string]any{
				"chat_session": map[string]any{"id": "test-session-id"},
			},
		},
	})
}

// newTestClient builds a Client wired to the given server URL with the WASM
// solver replaced by a mock so tests don't depend on probabilistic PoW solving.
func newTestClient(t *testing.T, baseURL string) *Client {
	t.Helper()
	c, err := NewClient(HeaderConfig{Token: "test-token"}, nil, baseURL, 30*time.Second)
	if err != nil {
		t.Fatalf("NewClient: %v", err)
	}
	c.powSupplier = mockPowSupplier
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

func TestSendChat_RetryOn40301(t *testing.T) {
	attempt := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionPath:
			sessionHandler(w, r)
		case chatPath:
			attempt++
			if attempt == 1 {
				// First attempt: simulate expired PoW.
				w.Header().Set("Content-Type", "application/json")
				_ = json.NewEncoder(w).Encode(map[string]any{
					"code": 40301,
					"msg":  "INVALID_POW_RESPONSE",
				})
				return
			}
			// Second attempt: return a minimal SSE success.
			w.Header().Set("Content-Type", "text/event-stream")
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write([]byte("data: {}\n\n"))
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	req := &Request{Prompt: "hello", RefFileIDs: []string{}, ModelType: "default"}

	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("expected retry to succeed, got: %v", err)
	}
	defer resp.Body.Close()

	if attempt != 2 {
		t.Errorf("expected exactly 2 chat attempts, got %d", attempt)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "text/event-stream" {
		t.Errorf("expected SSE on success, got %q", ct)
	}
}

func TestSendChat_HardErrorOn40300(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionPath:
			sessionHandler(w, r)
		case chatPath:
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{
				"code": 40300,
				"msg":  "MISSING_HEADER",
			})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	req := &Request{Prompt: "hello", RefFileIDs: []string{}, ModelType: "default"}

	_, err := c.Do(context.Background(), req)
	if err == nil {
		t.Fatal("expected hard error on 40300, got nil")
	}
	if !containsStr(err.Error(), "40300") {
		t.Errorf("error should mention code 40300, got: %v", err)
	}
}

func TestSendChat_NoRetryOnOtherCodes(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case sessionPath:
			sessionHandler(w, r)
		case chatPath:
			attempts++
			w.Header().Set("Content-Type", "application/json")
			_ = json.NewEncoder(w).Encode(map[string]any{"code": 500, "msg": "internal error"})
		}
	}))
	defer srv.Close()

	c := newTestClient(t, srv.URL)
	req := &Request{Prompt: "hello", RefFileIDs: []string{}, ModelType: "default"}

	resp, err := c.Do(context.Background(), req)
	if err != nil {
		t.Fatalf("unexpected error on non-40301 JSON: %v", err)
	}
	defer resp.Body.Close()
	if attempts != 1 {
		t.Errorf("expected exactly 1 attempt for non-retryable code, got %d", attempts)
	}
}

func containsStr(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}
