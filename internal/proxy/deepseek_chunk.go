package proxy

import (
	"encoding/json"
)

// streamState tracks parsing state across chunks of a single DeepSeek SSE response.
type streamState struct {
	// fragmentType is the type of the most recently appended fragment.
	// "THINK" fragments are reasoning content (suppressed); "RESPONSE" is the user-visible answer.
	fragmentType string
}

// chunkResult captures the user-visible effect of a single DeepSeek SSE chunk.
type chunkResult struct {
	Delta    string
	Finished bool
	// CompletionTokens is populated from DeepSeek's accumulated_token_usage field
	// when it appears in a BATCH operation. Zero means the field was absent.
	CompletionTokens int
}

// parseDeepSeekChunk decodes one chunk JSON from DeepSeek's SSE stream and
// returns the content delta to forward + whether this signals end-of-stream.
//
// DeepSeek encodes its response as a series of JSON-Patch-like operations
// over a logical {response: {fragments: [...]}} document:
//
//   - {"v": {"response": {...fragments: [{type, content}]}}}        initial state
//   - {"p": "response/fragments", "o": "APPEND", "v": [{type, content}]}  new fragment
//   - {"p": "response/fragments/-1/content", "v": "text"}            append to last fragment
//   - {"v": "text"}                                                   shortcut: same as above
//   - {"p": "response/status", "v": "FINISHED"}                       end of stream
//   - {"p": "response", "o": "BATCH", "v": [{p, v}, ...]}             batched ops (look for quasi_status)
//
// Only RESPONSE-typed fragments produce visible deltas; THINK fragments are
// skipped because they correspond to DeepSeek's internal reasoning trace.
func parseDeepSeekChunk(raw []byte, state *streamState) (chunkResult, error) {
	var chunk struct {
		P json.RawMessage `json:"p"`
		O json.RawMessage `json:"o"`
		V json.RawMessage `json:"v"`
	}
	if err := json.Unmarshal(raw, &chunk); err != nil {
		return chunkResult{}, err
	}

	// Path-based op: chunk targets a specific JSON path.
	if len(chunk.P) > 0 {
		var path string
		if err := json.Unmarshal(chunk.P, &path); err != nil {
			return chunkResult{}, nil
		}
		return state.handlePathOp(path, chunk.O, chunk.V), nil
	}

	if len(chunk.V) == 0 {
		return chunkResult{}, nil
	}

	// No "p" field — either initial state object or implicit content append.

	// Try parsing v as the initial response object.
	var vObj struct {
		Response struct {
			Fragments []fragment `json:"fragments"`
		} `json:"response"`
	}
	if err := json.Unmarshal(chunk.V, &vObj); err == nil && len(vObj.Response.Fragments) > 0 {
		f := vObj.Response.Fragments[0]
		state.fragmentType = f.Type
		if f.Type == "RESPONSE" {
			return chunkResult{Delta: f.Content}, nil
		}
		return chunkResult{}, nil
	}

	// Otherwise v should be a plain string — implicit append to last fragment.
	var vStr string
	if err := json.Unmarshal(chunk.V, &vStr); err == nil {
		if state.fragmentType == "RESPONSE" {
			return chunkResult{Delta: vStr}, nil
		}
	}
	return chunkResult{}, nil
}

type fragment struct {
	Type    string `json:"type"`
	Content string `json:"content"`
}

func (s *streamState) handlePathOp(path string, o, v json.RawMessage) chunkResult {
	switch path {
	case "response/fragments":
		// A new fragment is being appended (THINK or RESPONSE).
		var frags []fragment
		if err := json.Unmarshal(v, &frags); err != nil || len(frags) == 0 {
			return chunkResult{}
		}
		s.fragmentType = frags[0].Type
		if frags[0].Type == "RESPONSE" {
			return chunkResult{Delta: frags[0].Content}
		}

	case "response/fragments/-1/content":
		// Direct append/set on the last fragment's content.
		var str string
		if err := json.Unmarshal(v, &str); err != nil {
			return chunkResult{}
		}
		if s.fragmentType == "RESPONSE" {
			return chunkResult{Delta: str}
		}

	case "response/status":
		var str string
		if err := json.Unmarshal(v, &str); err == nil && str == "FINISHED" {
			return chunkResult{Finished: true}
		}

	case "response":
		// Batched ops — scan for quasi_status FINISHED and accumulated_token_usage.
		var op string
		if err := json.Unmarshal(o, &op); err != nil || op != "BATCH" {
			return chunkResult{}
		}
		var batch []struct {
			P string          `json:"p"`
			V json.RawMessage `json:"v"`
		}
		if err := json.Unmarshal(v, &batch); err != nil {
			return chunkResult{}
		}
		result := chunkResult{}
		for _, item := range batch {
			switch item.P {
			case "quasi_status":
				var str string
				if err := json.Unmarshal(item.V, &str); err == nil && str == "FINISHED" {
					result.Finished = true
				}
			case "accumulated_token_usage":
				var n int
				if err := json.Unmarshal(item.V, &n); err == nil && n > 0 {
					result.CompletionTokens = n
				}
			}
		}
		return result
	}
	return chunkResult{}
}
