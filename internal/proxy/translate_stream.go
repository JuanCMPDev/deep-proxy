package proxy

import (
	"bufio"
	"bytes"
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

var (
	sseDataPrefix = []byte("data: ")
	sseDoneMarker = []byte("[DONE]")
)

// translateStream reads DeepSeek's SSE response, extracts RESPONSE-fragment
// deltas via parseDeepSeekChunk, and forwards each one to the client as an
// OpenAI chat.completion.chunk SSE event.
func translateStream(
	ctx context.Context,
	upstream *http.Response,
	sse *openai.SSEWriter,
	requestedModel string,
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
	finishStr := "stop"
	roleSent := false

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
			// Emit the terminating chunk with finish_reason=stop.
			chunk.Choices[0].Delta = openai.ChunkDelta{}
			chunk.Choices[0].FinishReason = &finishStr
			if err := sse.WriteChunk(chunk); err != nil {
				return fmt.Errorf("write final chunk: %w", err)
			}
			sse.WriteDone()
			return nil
		}

		if result.Delta == "" {
			continue
		}

		// First content chunk also carries the assistant role per OpenAI spec.
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

	// Stream ended without an explicit FINISHED — emit a synthetic stop.
	chunk.Choices[0].Delta = openai.ChunkDelta{}
	chunk.Choices[0].FinishReason = &finishStr
	_ = sse.WriteChunk(chunk)
	sse.WriteDone()
	return nil
}
