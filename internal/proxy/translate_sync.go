package proxy

import (
	"bufio"
	"bytes"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

// translateSync reads DeepSeek's SSE response (the web API always streams),
// accumulates all RESPONSE-fragment deltas, and returns a single OpenAI ChatResponse.
func translateSync(upstream *http.Response, requestedModel string) (*openai.ChatResponse, error) {
	defer upstream.Body.Close()

	scanner := bufio.NewScanner(upstream.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	var (
		content          strings.Builder
		state            streamState
		finished         bool
		completionTokens int
	)

	for scanner.Scan() {
		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue
		}
		payload := bytes.TrimPrefix(line, sseDataPrefix)
		if bytes.Equal(payload, sseDoneMarker) {
			break
		}

		result, err := parseDeepSeekChunk(payload, &state)
		if err != nil {
			slog.Debug("unparseable chunk (sync)", slog.String("raw", string(payload)))
			continue
		}

		if result.Delta != "" {
			content.WriteString(result.Delta)
		}
		if result.CompletionTokens > 0 {
			completionTokens = result.CompletionTokens
		}
		if result.Finished {
			finished = true
			// Continue draining — server may send a few trailing events
			// (title, close) before closing the connection.
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read upstream stream: %w", err)
	}

	finishReason := "stop"
	if !finished && content.Len() == 0 {
		finishReason = "error"
	}

	return &openai.ChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMicro()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestedModel,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      openai.Message{Role: "assistant", Content: content.String()},
			FinishReason: finishReason,
		}},
		Usage: openai.Usage{
			// prompt_tokens is not exposed by DeepSeek's web API; only output is tracked.
			CompletionTokens: completionTokens,
			TotalTokens:      completionTokens,
		},
	}, nil
}
