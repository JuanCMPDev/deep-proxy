package openai

import "encoding/json"

// Message is one entry in the chat history.
// Role is "system" | "user" | "assistant" | "tool".
//
// For role="assistant" messages that issued tool calls, ToolCalls is populated
// and Content may be empty.
//
// For role="tool" messages (the result of executing a tool call), ToolCallID
// must reference the assistant message's tool call ID and Content is the
// stringified tool output.
type Message struct {
	Role       string     `json:"role"`
	Content    string     `json:"content,omitempty"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`
	ToolCallID string     `json:"tool_call_id,omitempty"`
	Name       string     `json:"name,omitempty"`
}

// Tool is a function the model is allowed to call.
type Tool struct {
	Type     string       `json:"type"` // "function"
	Function ToolFunction `json:"function"`
}

// ToolFunction describes a callable function.
type ToolFunction struct {
	Name        string          `json:"name"`
	Description string          `json:"description,omitempty"`
	Parameters  json.RawMessage `json:"parameters,omitempty"`
}

// ToolCall is a model-issued request to invoke a function.
type ToolCall struct {
	ID       string           `json:"id"`
	Type     string           `json:"type"` // "function"
	Function ToolCallFunction `json:"function"`
}

type ToolCallFunction struct {
	Name string `json:"name"`
	// Arguments is a JSON string (per OpenAI spec) — the model emits a JSON
	// object, but it's transported as a string-encoded JSON for streaming.
	Arguments string `json:"arguments"`
}

type ChatRequest struct {
	Model       string    `json:"model"`
	Messages    []Message `json:"messages"`
	Stream      bool      `json:"stream,omitempty"`
	Temperature *float64  `json:"temperature,omitempty"`
	TopP        *float64  `json:"top_p,omitempty"`
	MaxTokens   *int      `json:"max_tokens,omitempty"`
	Tools       []Tool    `json:"tools,omitempty"`
	ToolChoice  any       `json:"tool_choice,omitempty"`
}

type Choice struct {
	Index        int     `json:"index"`
	Message      Message `json:"message"`
	FinishReason string  `json:"finish_reason"`
}

type Usage struct {
	PromptTokens     int `json:"prompt_tokens"`
	CompletionTokens int `json:"completion_tokens"`
	TotalTokens      int `json:"total_tokens"`
}

type ChatResponse struct {
	ID      string   `json:"id"`
	Object  string   `json:"object"`
	Created int64    `json:"created"`
	Model   string   `json:"model"`
	Choices []Choice `json:"choices"`
	Usage   Usage    `json:"usage"`
}

type ErrorDetail struct {
	Message string `json:"message"`
	Type    string `json:"type"`
	Code    string `json:"code,omitempty"`
}

type ErrorResponse struct {
	Error ErrorDetail `json:"error"`
}

// Streaming types — used by the SSE path.

// ChunkDelta is the per-chunk update to the assistant's message.
// Only the changed fields are populated.
type ChunkDelta struct {
	Role      string          `json:"role,omitempty"`
	Content   string          `json:"content,omitempty"`
	ToolCalls []ChunkToolCall `json:"tool_calls,omitempty"`
}

// ChunkToolCall is the streaming form of a tool call.
// On the first delta, ID/Type/Function.Name are populated.
// On subsequent deltas, only Function.Arguments accumulates.
type ChunkToolCall struct {
	Index    int                 `json:"index"`
	ID       string              `json:"id,omitempty"`
	Type     string              `json:"type,omitempty"`
	Function ChunkToolCallFunc   `json:"function,omitempty"`
}

type ChunkToolCallFunc struct {
	Name      string `json:"name,omitempty"`
	Arguments string `json:"arguments,omitempty"`
}

type ChunkChoice struct {
	Index        int        `json:"index"`
	Delta        ChunkDelta `json:"delta"`
	FinishReason *string    `json:"finish_reason"`
}

type ChatChunk struct {
	ID      string        `json:"id"`
	Object  string        `json:"object"`
	Created int64         `json:"created"`
	Model   string        `json:"model"`
	Choices []ChunkChoice `json:"choices"`
}
