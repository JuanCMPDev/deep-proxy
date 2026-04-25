package openai

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
)

// SSEWriter writes OpenAI-compatible server-sent events to an HTTP response.
// All headers are committed in NewSSEWriter; callers must not touch w.Header() after that.
type SSEWriter struct {
	w       http.ResponseWriter
	flusher http.Flusher
	buf     bytes.Buffer
}

// NewSSEWriter asserts flusher support, sets all required SSE headers, sends the
// 200 status, and flushes — so the client establishes the stream before the first
// token arrives.
func NewSSEWriter(w http.ResponseWriter) (*SSEWriter, error) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		return nil, fmt.Errorf("response writer does not implement http.Flusher")
	}

	h := w.Header()
	h.Set("Content-Type", "text/event-stream")
	h.Set("Cache-Control", "no-cache, no-transform")
	h.Set("Connection", "keep-alive")
	h.Set("X-Accel-Buffering", "no") // disables nginx proxy buffering
	w.WriteHeader(http.StatusOK)
	flusher.Flush()

	return &SSEWriter{w: w, flusher: flusher}, nil
}

// WriteChunk serialises chunk as a single SSE data event and flushes immediately.
// The buf field is reused across calls to avoid per-chunk heap allocation.
func (s *SSEWriter) WriteChunk(chunk *ChatChunk) error {
	s.buf.Reset()
	s.buf.WriteString("data: ")

	// SetEscapeHTML(false) preserves < > & in generated code/content verbatim.
	enc := json.NewEncoder(&s.buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(chunk); err != nil {
		return err
	}
	// Encode appends '\n'; add a second '\n' for the SSE event boundary.
	s.buf.WriteByte('\n')

	if _, err := s.w.Write(s.buf.Bytes()); err != nil {
		return err
	}
	s.flusher.Flush()
	return nil
}

// WriteDone sends the terminal SSE event. Errors are intentionally swallowed —
// if the client disconnected we can't send anything anyway.
func (s *SSEWriter) WriteDone() {
	_, _ = fmt.Fprint(s.w, "data: [DONE]\n\n")
	s.flusher.Flush()
}
