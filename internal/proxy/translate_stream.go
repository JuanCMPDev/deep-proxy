package proxy

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

var (
	sseDataPrefix = []byte("data: ")
	sseDoneMarker = []byte("[DONE]")
)

// translateStream reads DeepSeek's SSE response, extracts RESPONSE-fragment
// deltas, and forwards them to the client as OpenAI chat.completion.chunk
// events.
//
// When toolMode is true the entire response is buffered, parsed for
// <tool_call> blocks, and emitted as either a single tool_calls delta
// (if any tools were called) or as a regular content block. Streaming with
// tools is intentionally degenerate-to-buffered for v1 simplicity; OpenCode
// and similar agents don't depend on token-by-token streaming during tool
// invocation.
func translateStream(
	ctx context.Context,
	upstream *http.Response,
	sse *openai.SSEWriter,
	requestedModel string,
	toolMode bool,
) error {
	defer upstream.Body.Close()

	scanner := bufio.NewScanner(upstream.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 512*1024)

	chunk := &openai.ChatChunk{
		ID:      fmt.Sprintf("chatcmpl-%d", time.Now().UnixMicro()),
		Object:  "chat.completion.chunk",
		Model:   requestedModel,
		Created: time.Now().Unix(),
		Choices: []openai.ChunkChoice{{Index: 0}},
	}

	state := &streamState{}
	stopFinish := "stop"
	toolFinish := "tool_calls"
	roleSent := false

	// In tool mode we accumulate everything; we'll decide at the end whether
	// to emit a tool_calls delta or normal content.
	var buffered strings.Builder

	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return err
		}

		line := scanner.Bytes()
		if !bytes.HasPrefix(line, sseDataPrefix) {
			continue
		}
		payload := bytes.TrimPrefix(line, sseDataPrefix)
		if bytes.Equal(payload, sseDoneMarker) {
			break
		}

		result, err := parseDeepSeekChunk(payload, state)
		if err != nil {
			slog.Debug("unparseable chunk", slog.String("raw", string(payload)))
			continue
		}

		if result.Finished {
			break
		}

		if result.Delta == "" {
			continue
		}

		if toolMode {
			buffered.WriteString(result.Delta)
			continue
		}

		// Pass-through streaming for non-tool requests.
		if !roleSent {
			chunk.Choices[0].Delta = openai.ChunkDelta{Role: "assistant", Content: result.Delta}
			roleSent = true
		} else {
			chunk.Choices[0].Delta = openai.ChunkDelta{Content: result.Delta}
		}
		chunk.Choices[0].FinishReason = nil

		if err := sse.WriteChunk(chunk); err != nil {
			return fmt.Errorf("write chunk to client: %w", err)
		}
	}

	if err := scanner.Err(); err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("read upstream stream: %w", err)
	}

	if !toolMode {
		// Plain finish.
		chunk.Choices[0].Delta = openai.ChunkDelta{}
		chunk.Choices[0].FinishReason = &stopFinish
		_ = sse.WriteChunk(chunk)
		sse.WriteDone()
		return nil
	}

	// Tool mode: parse the buffered content for <tool_call> blocks.
	cleanContent, toolCalls := extractToolCalls(buffered.String())

	if len(toolCalls) == 0 {
		// No tools — emit the whole thing as one assistant content delta.
		if cleanContent != "" {
			chunk.Choices[0].Delta = openai.ChunkDelta{Role: "assistant", Content: cleanContent}
			chunk.Choices[0].FinishReason = nil
			if err := sse.WriteChunk(chunk); err != nil {
				return fmt.Errorf("write content delta: %w", err)
			}
		}
		chunk.Choices[0].Delta = openai.ChunkDelta{}
		chunk.Choices[0].FinishReason = &stopFinish
		_ = sse.WriteChunk(chunk)
		sse.WriteDone()
		return nil
	}

	// Emit any pre-tool reasoning text first (if model included some).
	if cleanContent != "" {
		chunk.Choices[0].Delta = openai.ChunkDelta{Role: "assistant", Content: cleanContent}
		chunk.Choices[0].FinishReason = nil
		if err := sse.WriteChunk(chunk); err != nil {
			return fmt.Errorf("write content delta: %w", err)
		}
		roleSent = true
	}

	// Emit each tool_call as a streaming delta.
	for i, tc := range toolCalls {
		delta := openai.ChunkDelta{}
		if !roleSent && i == 0 {
			delta.Role = "assistant"
			roleSent = true
		}
		delta.ToolCalls = []openai.ChunkToolCall{{
			Index: i,
			ID:    tc.ID,
			Type:  tc.Type,
			Function: openai.ChunkToolCallFunc{
				Name:      tc.Function.Name,
				Arguments: tc.Function.Arguments,
			},
		}}
		chunk.Choices[0].Delta = delta
		chunk.Choices[0].FinishReason = nil
		if err := sse.WriteChunk(chunk); err != nil {
			return fmt.Errorf("write tool_call delta: %w", err)
		}
	}

	// Final terminator chunk with finish_reason=tool_calls.
	chunk.Choices[0].Delta = openai.ChunkDelta{}
	chunk.Choices[0].FinishReason = &toolFinish
	_ = sse.WriteChunk(chunk)
	sse.WriteDone()
	return nil
}

// debugDumpRequest is a helper kept in case we want to log the OpenAI request
// shape during development. Not wired in by default.
func debugDumpRequest(req any) string {
	b, _ := json.Marshal(req)
	return string(b)
}
