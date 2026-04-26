package proxy

import (
	"encoding/json"
	"fmt"
	"strings"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
	"github.com/JuanCMPDev/deep-proxy/internal/upstream"
)

// modelTypeMap converts OpenAI-style model names to DeepSeek's internal model_type field.
var modelTypeMap = map[string]string{
	"deepseek-chat":     "default",
	"deepseek-v3":       "default",
	"deepseek-reasoner": "expert",
	"deepseek-r1":       "expert",
	"deepseek-expert":   "expert",
}

func resolveModelType(requested string) string {
	if t, ok := modelTypeMap[requested]; ok {
		return t
	}
	return "default"
}

// toolInstructionsHeader is appended to the system prompt when the request
// includes a tools array. It teaches the model the exact wire format
// translate_sync.go / translate_stream.go know how to parse back.
const toolInstructionsHeader = `You have access to the following tools. To call a tool, emit a block with this EXACT format anywhere in your response:

<tool_call name="TOOL_NAME">{"arg1": "value1", "arg2": "value2"}</tool_call>

Rules:
- The JSON inside must be valid and contain ONLY the arguments object.
- You may emit MULTIPLE <tool_call> blocks in one response if needed.
- Provide brief reasoning text BEFORE the tool_call blocks if useful.
- If no tool is needed, respond with plain text only.

Available tools:`

// formatToolDefinitions renders OpenAI tool definitions as a human-readable
// list the model can use as a reference.
func formatToolDefinitions(tools []openai.Tool) string {
	if len(tools) == 0 {
		return ""
	}
	var sb strings.Builder
	sb.WriteString(toolInstructionsHeader)
	sb.WriteString("\n")
	for _, t := range tools {
		fn := t.Function
		sb.WriteString("- ")
		sb.WriteString(fn.Name)
		if fn.Description != "" {
			sb.WriteString(": ")
			sb.WriteString(fn.Description)
		}
		if len(fn.Parameters) > 0 {
			sb.WriteString("\n  parameters: ")
			sb.Write(compactJSONBytes(fn.Parameters))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

func compactJSONBytes(raw json.RawMessage) []byte {
	var v any
	if err := json.Unmarshal(raw, &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return out
}

// messagesToPrompt flattens an OpenAI messages array (with optional tool calls
// and tool results) into a single prompt string DeepSeek's web API can ingest.
//
// Rendering rules:
//   - system / user / assistant text → tagged sections
//   - assistant.tool_calls → re-rendered as <tool_call> blocks so the model
//     sees its own previous calls in the conversation history
//   - role="tool" → rendered as a [Tool result for X] block
func messagesToPrompt(msgs []openai.Message, tools []openai.Tool) string {
	if len(msgs) == 0 && len(tools) == 0 {
		return ""
	}

	var sb strings.Builder

	// If there are tools, prepend the tool instructions as a system note.
	// We attach them BEFORE the first message so they live with the system context.
	if len(tools) > 0 {
		sb.WriteString("[System]: ")
		sb.WriteString(formatToolDefinitions(tools))
		sb.WriteString("\n\n")
	}

	for _, m := range msgs {
		switch m.Role {
		case "system":
			sb.WriteString("[System]: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "user":
			sb.WriteString("[User]: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		case "assistant":
			sb.WriteString("[Assistant]: ")
			if m.Content != "" {
				sb.WriteString(m.Content)
			}
			for _, tc := range m.ToolCalls {
				if sb.Len() > 0 && !strings.HasSuffix(sb.String(), "\n") {
					sb.WriteString("\n")
				}
				fmt.Fprintf(&sb, `<tool_call name="%s">%s</tool_call>`, tc.Function.Name, tc.Function.Arguments)
			}
			sb.WriteString("\n\n")
		case "tool":
			name := m.Name
			if name == "" {
				name = "tool"
			}
			fmt.Fprintf(&sb, "[Tool result for %s]: %s\n\n", name, m.Content)
		default:
			sb.WriteString("[")
			sb.WriteString(m.Role)
			sb.WriteString("]: ")
			sb.WriteString(m.Content)
			sb.WriteString("\n\n")
		}
	}

	return strings.TrimRight(sb.String(), "\n")
}

func toUpstreamRequest(req *openai.ChatRequest, thinkingEnabled, searchEnabled bool) *upstream.Request {
	return &upstream.Request{
		ChatSessionID:   "",
		ParentMessageID: nil,
		ModelType:       resolveModelType(req.Model),
		Prompt:          messagesToPrompt(req.Messages, req.Tools),
		RefFileIDs:      []string{},
		ThinkingEnabled: thinkingEnabled,
		SearchEnabled:   searchEnabled,
		Preempt:         false,
	}
}
