package proxy

import (
	"testing"
)

func TestParseDeepSeekChunk_CompletionTokens(t *testing.T) {
	// BATCH event carrying both quasi_status FINISHED and accumulated_token_usage.
	raw := []byte(`{"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":43},{"p":"quasi_status","v":"FINISHED"}]}`)
	state := &streamState{}
	result, err := parseDeepSeekChunk(raw, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !result.Finished {
		t.Error("expected Finished=true")
	}
	if result.CompletionTokens != 43 {
		t.Errorf("expected CompletionTokens=43, got %d", result.CompletionTokens)
	}
}

func TestParseDeepSeekChunk_CompletionTokensWithoutFinished(t *testing.T) {
	// BATCH with only token count (no quasi_status yet).
	raw := []byte(`{"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":10}]}`)
	state := &streamState{}
	result, err := parseDeepSeekChunk(raw, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.Finished {
		t.Error("expected Finished=false")
	}
	if result.CompletionTokens != 10 {
		t.Errorf("expected CompletionTokens=10, got %d", result.CompletionTokens)
	}
}

func TestParseDeepSeekChunk_ZeroTokensIgnored(t *testing.T) {
	// Zero value is a sentinel meaning "not reported yet" — must not overwrite.
	raw := []byte(`{"p":"response","o":"BATCH","v":[{"p":"accumulated_token_usage","v":0}]}`)
	state := &streamState{}
	result, err := parseDeepSeekChunk(raw, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result.CompletionTokens != 0 {
		t.Errorf("zero usage should not be stored, got %d", result.CompletionTokens)
	}
}
