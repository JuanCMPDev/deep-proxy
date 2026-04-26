package proxy

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/auth"
	"github.com/JuanCMPDev/deep-proxy/internal/config"
	"github.com/JuanCMPDev/deep-proxy/internal/openai"
	"github.com/JuanCMPDev/deep-proxy/internal/upstream"
)

type Handler struct {
	cfg    *config.Config
	client *upstream.Client
	log    *slog.Logger
}

func NewHandler(cfg *config.Config, store *auth.HeaderStore, log *slog.Logger) (*Handler, error) {
	headers := upstream.HeaderConfig{
		Token:          cfg.Token,
		UserAgent:      cfg.UserAgent,
		AppVersion:     cfg.AppVersion,
		ClientVersion:  cfg.ClientVersion,
		ClientPlatform: cfg.ClientPlatform,
		ClientLocale:   cfg.ClientLocale,
		TimezoneOffset: cfg.TimezoneOffset,
		Cookie:         cfg.Cookie,
		PowResponse:    cfg.PowResponse,
		HifLeim:        cfg.HifLeim,
	}
	client, err := upstream.NewClient(headers, store, cfg.UpstreamBase, cfg.Timeout)
	if err != nil {
		return nil, err
	}
	return &Handler{
		cfg:    cfg,
		client: client,
		log:    log,
	}, nil
}

func (h *Handler) Chat(w http.ResponseWriter, r *http.Request) {
	start := time.Now()

	var req openai.ChatRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "malformed JSON body")
		return
	}

	if len(req.Messages) == 0 {
		writeError(w, http.StatusBadRequest, "invalid_request", "messages must not be empty")
		return
	}

	// Model field is optional from OpenAI clients — fall back to configured default.
	if req.Model == "" {
		req.Model = h.cfg.Model
	}

	dsReq := toUpstreamRequest(&req, h.cfg.ThinkingEnabled, h.cfg.SearchEnabled)

	resp, err := h.client.Do(r.Context(), dsReq)
	if err != nil {
		h.log.Error("upstream request failed", slog.String("error", err.Error()))
		writeError(w, http.StatusBadGateway, "upstream_error", "failed to reach DeepSeek: "+err.Error())
		return
	}

	switch resp.StatusCode {
	case http.StatusUnauthorized:
		resp.Body.Close()
		writeError(w, http.StatusUnauthorized, "invalid_session",
			"DeepSeek session expired — refresh your token and restart")
		return
	case http.StatusTooManyRequests:
		retryAfter := resp.Header.Get("Retry-After")
		resp.Body.Close()
		if retryAfter != "" {
			w.Header().Set("Retry-After", retryAfter)
		}
		writeError(w, http.StatusTooManyRequests, "rate_limit_exceeded", "DeepSeek rate limit hit")
		return
	case http.StatusOK:
		// continue — streaming body is consumed downstream
	default:
		resp.Body.Close()
		writeError(w, http.StatusBadGateway, "upstream_error",
			"unexpected upstream status: "+resp.Status)
		return
	}

	if req.Stream {
		h.handleStream(w, r, resp, &req, start)
		return
	}
	h.handleSync(w, resp, &req, start)
}

// hasTools reports whether the request expects tool-call-aware response handling.
func hasTools(req *openai.ChatRequest) bool {
	return len(req.Tools) > 0
}

func (h *Handler) handleStream(
	w http.ResponseWriter,
	r *http.Request,
	resp *http.Response,
	req *openai.ChatRequest,
	start time.Time,
) {
	sse, err := openai.NewSSEWriter(w)
	if err != nil {
		resp.Body.Close()
		writeError(w, http.StatusInternalServerError, "streaming_error", err.Error())
		return
	}

	if err := translateStream(r.Context(), resp, sse, req.Model, hasTools(req)); err != nil {
		if !errors.Is(err, context.Canceled) {
			h.log.Warn("stream terminated with error", slog.String("error", err.Error()))
		}
		return
	}

	h.log.Info("stream done",
		slog.String("model", req.Model),
		slog.Int("messages", len(req.Messages)),
		slog.Bool("tools", hasTools(req)),
		slog.Duration("latency", time.Since(start)),
	)
}

func (h *Handler) handleSync(
	w http.ResponseWriter,
	resp *http.Response,
	req *openai.ChatRequest,
	start time.Time,
) {
	openaiResp, err := translateSync(resp, req.Model)
	if err != nil {
		h.log.Error("response translation failed", slog.String("error", err.Error()))
		writeError(w, http.StatusInternalServerError, "translation_error",
			"could not translate upstream response: "+err.Error())
		return
	}

	w.Header().Set("Content-Type", "application/json")
	if err := json.NewEncoder(w).Encode(openaiResp); err != nil {
		h.log.Error("encode response failed", slog.String("error", err.Error()))
		return
	}

	h.log.Info("ok",
		slog.String("model", req.Model),
		slog.Int("messages", len(req.Messages)),
		slog.Int("completion_tokens", openaiResp.Usage.CompletionTokens),
		slog.Duration("latency", time.Since(start)),
	)
}
