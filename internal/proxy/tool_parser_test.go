package proxy

import (
	"strings"
	"testing"
)

func TestExtractToolCalls_SingleCall(t *testing.T) {
	in := `I'll list the files first.

<tool_call name="Bash">{"command": "ls -la", "description": "List files"}</tool_call>`

	clean, calls := extractToolCalls(in)

	if len(calls) != 1 {
		t.Fatalf("expected 1 tool call, got %d", len(calls))
	}
	if calls[0].Function.Name != "Bash" {
		t.Errorf("expected Name=Bash, got %q", calls[0].Function.Name)
	}
	if !strings.Contains(calls[0].Function.Arguments, `"command":"ls -la"`) {
		t.Errorf("arguments should contain command field, got %q", calls[0].Function.Arguments)
	}
	if !strings.HasPrefix(calls[0].ID, "call_") {
		t.Errorf("ID should have call_ prefix, got %q", calls[0].ID)
	}
	if calls[0].Type != "function" {
		t.Errorf("Type should be 'function', got %q", calls[0].Type)
	}
	if !strings.Contains(clean, "I'll list the files first") {
		t.Errorf("cleaned content should preserve reasoning text, got %q", clean)
	}
	if strings.Contains(clean, "<tool_call") {
		t.Errorf("cleaned content should not contain <tool_call>, got %q", clean)
	}
}

func TestExtractToolCalls_MultipleCalls(t *testing.T) {
	in := `Doing two things:
<tool_call name="Read">{"file_path": "a.txt"}</tool_call>
<tool_call name="Read">{"file_path": "b.txt"}</tool_call>`

	_, calls := extractToolCalls(in)
	if len(calls) != 2 {
		t.Fatalf("expected 2 calls, got %d", len(calls))
	}
	if calls[0].Function.Name != "Read" || calls[1].Function.Name != "Read" {
		t.Errorf("both should be Read")
	}
	// IDs must be unique.
	if calls[0].ID == calls[1].ID {
		t.Errorf("call IDs must be unique, both %q", calls[0].ID)
	}
}

func TestExtractToolCalls_NoCalls(t *testing.T) {
	in := "Just a plain text response with no tools."
	clean, calls := extractToolCalls(in)
	if len(calls) != 0 {
		t.Errorf("expected 0 calls, got %d", len(calls))
	}
	if clean != in {
		t.Errorf("content should pass through unchanged")
	}
}

func TestExtractToolCalls_ArgumentsAreCompactedJSON(t *testing.T) {
	in := `<tool_call name="X">{
		"a": 1,
		"b":   "two"
	}</tool_call>`
	_, calls := extractToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call")
	}
	// Arguments should be compact JSON (no whitespace).
	args := calls[0].Function.Arguments
	if strings.ContainsAny(args, "\n\t") {
		t.Errorf("arguments should be compacted, got %q", args)
	}
	if args != `{"a":1,"b":"two"}` {
		t.Errorf("expected compact JSON, got %q", args)
	}
}

func TestExtractToolCalls_MalformedJSONFallsBack(t *testing.T) {
	// Malformed JSON inside the block — should still extract but pass JSON through verbatim.
	in := `<tool_call name="X">{not valid json}</tool_call>`
	_, calls := extractToolCalls(in)
	if len(calls) != 1 {
		t.Fatalf("expected 1 call even with bad JSON")
	}
	// We pass through the raw payload when JSON parsing fails.
	if calls[0].Function.Arguments != "{not valid json}" {
		t.Errorf("expected raw passthrough, got %q", calls[0].Function.Arguments)
	}
}
