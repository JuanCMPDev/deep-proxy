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

// translateSync reads DeepSeek's SSE response, accumulates RESPONSE-fragment
// deltas, optionally parses any <tool_call> blocks, and returns one OpenAI
// ChatResponse.
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
		}
	}

	if err := scanner.Err(); err != nil {
		return nil, fmt.Errorf("read upstream stream: %w", err)
	}

	rawContent := content.String()
	cleanContent, toolCalls := extractToolCalls(rawContent)

	finishReason := "stop"
	switch {
	case len(toolCalls) > 0:
		finishReason = "tool_calls"
	case !finished && cleanContent == "":
		finishReason = "error"
	}

	msg := openai.Message{Role: "assistant"}
	if cleanContent != "" {
		msg.Content = cleanContent
	}
	if len(toolCalls) > 0 {
		msg.ToolCalls = toolCalls
	}

	return &openai.ChatResponse{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMicro()),
		Object:  "chat.completion",
		Created: time.Now().Unix(),
		Model:   requestedModel,
		Choices: []openai.Choice{{
			Index:        0,
			Message:      msg,
			FinishReason: finishReason,
		}},
		Usage: openai.Usage{
			CompletionTokens: completionTokens,
			TotalTokens:      completionTokens,
		},
	}, nil
}
