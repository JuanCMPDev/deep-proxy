package proxy

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"regexp"
	"strings"

	"github.com/JuanCMPDev/deep-proxy/internal/openai"
)

// toolCallPattern matches a single <tool_call name="X">JSON</tool_call> block.
// The model emits this format when prompted via the system instructions in
// translate_request.go. Whitespace inside the block is permitted.
var toolCallPattern = regexp.MustCompile(`(?s)<tool_call\s+name="([^"]+)"\s*>\s*(\{.*?\})\s*</tool_call>`)

// extractToolCalls scans the assistant content for <tool_call> blocks and
// returns:
//   - the cleaned content with all <tool_call> blocks removed (trimmed)
//   - the list of parsed tool calls in OpenAI format
//
// The arguments JSON is preserved verbatim as a string (OpenAI spec — the
// model's argument object is transported as stringified JSON, not nested).
func extractToolCalls(content string) (string, []openai.ToolCall) {
	matches := toolCallPattern.FindAllStringSubmatchIndex(content, -1)
	if len(matches) == 0 {
		return content, nil
	}

	var (
		cleaned strings.Builder
		calls   = make([]openai.ToolCall, 0, len(matches))
		last    int
	)

	for _, m := range matches {
		// Indices: m[0:1] full match, m[2:3] name, m[4:5] JSON.
		cleaned.WriteString(content[last:m[0]])
		last = m[1]

		name := content[m[2]:m[3]]
		argsRaw := content[m[4]:m[5]]

		args := compactJSON(argsRaw)

		calls = append(calls, openai.ToolCall{
			ID:   "call_" + randomID(),
			Type: "function",
			Function: openai.ToolCallFunction{
				Name:      name,
				Arguments: args,
			},
		})
	}
	cleaned.WriteString(content[last:])

	return strings.TrimSpace(cleaned.String()), calls
}

// compactJSON re-emits the arguments JSON without whitespace so the
// stringified arguments are stable. Falls back to the raw input if parse fails.
func compactJSON(raw string) string {
	var v any
	if err := json.Unmarshal([]byte(raw), &v); err != nil {
		return raw
	}
	out, err := json.Marshal(v)
	if err != nil {
		return raw
	}
	return string(out)
}

// randomID returns a 12-hex-char id suitable for OpenAI's call_<id> format.
func randomID() string {
	var b [6]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}
