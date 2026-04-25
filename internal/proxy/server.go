package proxy

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/auth"
	"github.com/JuanCMPDev/deep-proxy/internal/config"
)

func NewServer(cfg *config.Config, store *auth.HeaderStore, log *slog.Logger) (*http.Server, error) {
	h, err := NewHandler(cfg, store, log)
	if err != nil {
		return nil, err
	}

	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/chat/completions", h.Chat)
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		fmt.Fprintln(w, "ok")
	})

	return &http.Server{
		Addr:              cfg.Addr(),
		Handler:           mux,
		ReadHeaderTimeout: 5 * time.Second,
		IdleTimeout:       60 * time.Second,
	}, nil
}

func Serve(ctx context.Context, srv *http.Server, log *slog.Logger) error {
	errCh := make(chan error, 1)
	go func() {
		if err := srv.ListenAndServe(); err != http.ErrServerClosed {
			errCh <- err
		}
	}()

	select {
	case err := <-errCh:
		return err
	case <-ctx.Done():
		log.Info("shutting down")
		shutCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return srv.Shutdown(shutCtx)
	}
}
