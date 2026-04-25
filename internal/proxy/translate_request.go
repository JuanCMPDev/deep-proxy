package proxy

import (
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

// messagesToPrompt flattens an OpenAI messages array into a single prompt string.
// For a single user message the content is returned verbatim.
// For multi-turn conversations the history is formatted so DeepSeek has context.
func messagesToPrompt(msgs []openai.Message) string {
	if len(msgs) == 1 {
		return msgs[0].Content
	}

	var sb strings.Builder
	for _, m := range msgs {
		switch m.Role {
		case "system":
			sb.WriteString("[System]: ")
		case "user":
			sb.WriteString("[User]: ")
		case "assistant":
			sb.WriteString("[Assistant]: ")
		default:
			sb.WriteString("[" + m.Role + "]: ")
		}
		sb.WriteString(m.Content)
		sb.WriteString("\n\n")
	}
	return strings.TrimRight(sb.String(), "\n")
}

func toUpstreamRequest(req *openai.ChatRequest, thinkingEnabled, searchEnabled bool) *upstream.Request {
	// ChatSessionID is left empty here; the upstream Client will populate it by
	// calling /chat_session/create before dispatching the request.
	return &upstream.Request{
		ChatSessionID:   "",
		ParentMessageID: nil,
		ModelType:       resolveModelType(req.Model),
		Prompt:          messagesToPrompt(req.Messages),
		RefFileIDs:      []string{},
		ThinkingEnabled: thinkingEnabled,
		SearchEnabled:   searchEnabled,
		Preempt:         false,
	}
}
