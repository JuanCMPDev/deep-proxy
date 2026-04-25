package auth

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"
)

// RefreshFunc captures one set of credentials from a real browser session.
// Returns the bearer Token, Cookie header string, and x-hif-leim header value.
type RefreshFunc func(ctx context.Context) (token, cookie, hifLeim string, err error)

// Refresher periodically calls a RefreshFunc and writes the result into a
// HeaderStore so concurrent request handlers always see fresh credentials.
type Refresher struct {
	store    *HeaderStore
	refresh  RefreshFunc
	interval time.Duration

	mu     sync.Mutex
	cancel context.CancelFunc
	done   chan struct{}
}

func NewRefresher(store *HeaderStore, refresh RefreshFunc, interval time.Duration) *Refresher {
	return &Refresher{store: store, refresh: refresh, interval: interval}
}

// Start performs an initial refresh synchronously (bounded by 90s), then
// launches a background goroutine that refreshes on the configured interval.
func (r *Refresher) Start(ctx context.Context) error {
	initCtx, cancelInit := context.WithTimeout(ctx, 90*time.Second)
	token, cookie, hifLeim, err := r.refresh(initCtx)
	cancelInit()
	if err != nil {
		return fmt.Errorf("initial refresh: %w", err)
	}
	r.store.Set(token, cookie, hifLeim)
	slog.Info("credentials refreshed",
		slog.Bool("token", token != ""),
		slog.Int("cookie_len", len(cookie)),
		slog.Int("hif_leim_len", len(hifLeim)),
	)

	r.mu.Lock()
	r.done = make(chan struct{})
	loopCtx, cancel := context.WithCancel(ctx)
	r.cancel = cancel
	r.mu.Unlock()

	go r.loop(loopCtx)
	return nil
}

// Stop signals the background loop to exit and waits for it to finish.
func (r *Refresher) Stop() {
	r.mu.Lock()
	cancel := r.cancel
	done := r.done
	r.mu.Unlock()

	if cancel == nil {
		return
	}
	cancel()
	<-done
}

func (r *Refresher) loop(ctx context.Context) {
	defer close(r.done)

	ticker := time.NewTicker(r.interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			rctx, cancel := context.WithTimeout(ctx, 90*time.Second)
			token, cookie, hifLeim, err := r.refresh(rctx)
			cancel()

			if err != nil {
				if errors.Is(err, context.Canceled) {
					return
				}
				slog.Warn("credential refresh failed", slog.String("error", err.Error()))
				continue
			}
			r.store.Set(token, cookie, hifLeim)
			slog.Info("credentials refreshed",
				slog.Bool("token", token != ""),
				slog.Int("cookie_len", len(cookie)),
				slog.Int("hif_leim_len", len(hifLeim)),
			)
		}
	}
}
