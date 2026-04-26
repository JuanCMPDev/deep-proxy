package proxy

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

// fakeDeepSeekSSE wraps a list of SSE lines into an http.Response.
func fakeDeepSeekSSE(lines []string) *http.Response {
	body := strings.Join(lines, "\n") + "\n"
	rec := httptest.NewRecorder()
	rec.Body.WriteString(body)
	return rec.Result()
}

func TestTranslateStream_DeepSeekFormat(t *testing.T) {
	// Captured-shape lines from a real chat.deepseek.com response.
	// Two THINK fragment chunks (skipped), then a RESPONSE fragment
	// "Hello" + " world", then FINISHED.
	lines := []string{
		`data: {"v":{"response":{"message_id":2,"role":"ASSISTANT","fragments":[{"id":1,"type":"THINK","content":"thinking..."}]}}}`,
		`data: {"v":" more thinking"}`,
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":2,"type":"RESPONSE","content":"Hello"}]}`,
		`data: {"p":"response/fragments/-1/content","v":" world"}`,
		`data: {"p":"response/status","o":"SET","v":"FINISHED"}`,
	}

	upstream := fakeDeepSeekSSE(lines)
	rec := httptest.NewRecorder()
	sse, err := openai.NewSSEWriter(rec)
	if err != nil {
		t.Fatalf("NewSSEWriter: %v", err)
	}

	if err := translateStream(context.Background(), upstream, sse, "deepseek-chat", false); err != nil {
		t.Fatalf("translateStream: %v", err)
	}

	body := rec.Body.String()

	if !strings.Contains(body, `"Hello"`) {
		t.Errorf("expected 'Hello' delta in output, got:\n%s", body)
	}
	if !strings.Contains(body, `" world"`) {
		t.Errorf("expected ' world' delta in output, got:\n%s", body)
	}
	if !strings.Contains(body, `"role":"assistant"`) {
		t.Errorf("expected role on first chunk, got:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Errorf("expected finish_reason 'stop', got:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected [DONE] terminator, got:\n%s", body)
	}
	// THINK fragments must NOT leak into the visible output.
	if strings.Contains(body, "thinking...") || strings.Contains(body, "more thinking") {
		t.Errorf("THINK fragments leaked into client output:\n%s", body)
	}
}

func TestTranslateStream_BatchFinishSignal(t *testing.T) {
	lines := []string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":1,"type":"RESPONSE","content":"ok"}]}`,
		`data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":12},{"p":"quasi_status","v":"FINISHED"}]}`,
	}

	upstream := fakeDeepSeekSSE(lines)
	rec := httptest.NewRecorder()
	sse, _ := openai.NewSSEWriter(rec)

	if err := translateStream(context.Background(), upstream, sse, "deepseek-chat", false); err != nil {
		t.Fatalf("translateStream: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"ok"`) || !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Errorf("expected ok content and stop finish, got:\n%s", body)
	}
}

func TestTranslateStream_MalformedChunkSkipped(t *testing.T) {
	lines := []string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":1,"type":"RESPONSE","content":"a"}]}`,
		`data: {this is not json`,
		`data: {"v":"b"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}

	upstream := fakeDeepSeekSSE(lines)
	rec := httptest.NewRecorder()
	sse, _ := openai.NewSSEWriter(rec)

	if err := translateStream(context.Background(), upstream, sse, "deepseek-chat", false); err != nil {
		t.Fatalf("translateStream: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"a"`) || !strings.Contains(body, `"b"`) {
		t.Errorf("expected both valid chunks in output, got:\n%s", body)
	}
}

func TestTranslateSync_TokensExtracted(t *testing.T) {
	lines := []string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":1,"type":"RESPONSE","content":"hello"}]}`,
		`data: {"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":7},{"p":"quasi_status","v":"FINISHED"}]}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}

	upstream := fakeDeepSeekSSE(lines)
	resp, err := translateSync(upstream, "deepseek-chat")
	if err != nil {
		t.Fatalf("translateSync: %v", err)
	}
	if resp.Usage.CompletionTokens != 7 {
		t.Errorf("expected CompletionTokens=7, got %d", resp.Usage.CompletionTokens)
	}
	if resp.Usage.TotalTokens != 7 {
		t.Errorf("expected TotalTokens=7, got %d", resp.Usage.TotalTokens)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Errorf("expected content='hello', got %q", resp.Choices[0].Message.Content)
	}
}

func TestTranslateSync_ToolCallsExtracted(t *testing.T) {
	lines := []string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":1,"type":"RESPONSE","content":"I'll list files."}]}`,
		`data: {"v":" <tool_call name=\"Bash\">{\"command\":\"ls\"}</tool_call>"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}

	upstream := fakeDeepSeekSSE(lines)
	resp, err := translateSync(upstream, "deepseek-chat")
	if err != nil {
		t.Fatalf("translateSync: %v", err)
	}
	if resp.Choices[0].FinishReason != "tool_calls" {
		t.Errorf("expected finish_reason=tool_calls, got %q", resp.Choices[0].FinishReason)
	}
	if len(resp.Choices[0].Message.ToolCalls) != 1 {
		t.Fatalf("expected 1 tool_call in message, got %d", len(resp.Choices[0].Message.ToolCalls))
	}
	if resp.Choices[0].Message.ToolCalls[0].Function.Name != "Bash" {
		t.Errorf("expected Bash, got %q", resp.Choices[0].Message.ToolCalls[0].Function.Name)
	}
	if !strings.Contains(resp.Choices[0].Message.Content, "I'll list files") {
		t.Errorf("expected pre-tool reasoning preserved, got %q", resp.Choices[0].Message.Content)
	}
}

func TestTranslateStream_ToolModeBuffersAndEmits(t *testing.T) {
	lines := []string{
		`data: {"p":"response/fragments","o":"APPEND","v":[{"id":1,"type":"RESPONSE","content":""}]}`,
		`data: {"v":"<tool_call name=\"Read\">{\"file_path\":\"a.txt\"}</tool_call>"}`,
		`data: {"p":"response/status","v":"FINISHED"}`,
	}
	upstream := fakeDeepSeekSSE(lines)
	rec := httptest.NewRecorder()
	sse, _ := openai.NewSSEWriter(rec)

	if err := translateStream(context.Background(), upstream, sse, "deepseek-chat", true); err != nil {
		t.Fatalf("translateStream: %v", err)
	}

	body := rec.Body.String()
	if !strings.Contains(body, `"tool_calls"`) {
		t.Errorf("expected tool_calls in stream body, got:\n%s", body)
	}
	if !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Errorf("expected finish_reason=tool_calls, got:\n%s", body)
	}
	if !strings.Contains(body, "data: [DONE]") {
		t.Errorf("expected [DONE] terminator, got:\n%s", body)
	}
	if strings.Contains(body, "<tool_call") {
		t.Errorf("raw <tool_call> markup leaked to client:\n%s", body)
	}
}

func TestTranslateStream_ContextCancellation(t *testing.T) {
	pr, pw := io.Pipe()
	defer pw.Close()

	resp := &http.Response{
		StatusCode: http.StatusOK,
		Body:       pr,
	}

	rec := httptest.NewRecorder()
	sse, _ := openai.NewSSEWriter(rec)

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	go func() {
		<-ctx.Done()
		_ = pr.CloseWithError(ctx.Err())
	}()

	if err := translateStream(ctx, resp, sse, "deepseek-chat", false); err == nil {
		t.Fatal("expected context error, got nil")
	}
}
