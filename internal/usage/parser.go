package usage

import (
	"bufio"
	"io"
	"strings"

	"github.com/daneshih1125/ai.local/internal/apml"
	"github.com/tidwall/gjson"
)

// ParseStandardJSON extracts prompt and completion token counts from a standard,
// one-shot HTTP response body payload leveraging vendor-agnostic gjson dot paths.
func ParseStandardJSON(bodyBytes []byte, provider *apml.ProviderConfig) (int64, int64) {
	inputPath := provider.Usage.InputTokens
	outputPath := provider.Usage.OutputTokens
	inputTokens := gjson.GetBytes(bodyBytes, inputPath).Int()
	outpuTokens := gjson.GetBytes(bodyBytes, outputPath).Int()
	return inputTokens, outpuTokens
}

// ParseStreamReader reads SSE chunks from r and extracts token counts
// according to the provider's streaming config (split or last mode).
//
// This function blocks until the stream ends (EOF), so it must be called
// from a goroutine — never from the proxy hot path directly.
func ParseStreamReader(r io.Reader, provider *apml.ProviderConfig) (int64, int64) {
	if provider.Streaming == nil {
		return 0, 0
	}

	switch provider.Streaming.Mode {
	case "split":
		return parseSplitStream(r, provider.Streaming)
	case "last":
		return parseLastStream(r, provider.Streaming)
	default:
		return 0, 0
	}
}

// parseSplitStream handles Anthropic-style streaming where input tokens
// appear in one chunk type (e.g. message_start) and output tokens appear
// in another (e.g. message_delta).
//
// APML config example:
//
//	streaming:
//	  mode: split
//	  input:
//	    chunk_type: message_start
//	    input_tokens: message.usage.input_tokens
//	  output:
//	    chunk_type: message_delta
//	    output_tokens: usage.output_tokens
func parseSplitStream(r io.Reader, cfg *apml.StreamingConfig) (int64, int64) {
	if cfg.Input == nil || cfg.Output == nil {
		return 0, 0
	}

	var inputTokens, outputTokens int64

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		// SSE lines start with "data: ", skip empty lines and [DONE]
		jsonStr, ok := extractSSEData(line)
		if !ok {
			continue
		}

		chunkType := gjson.Get(jsonStr, "type").String()

		// input tokens chunk
		if chunkType == cfg.Input.ChunkType && cfg.Input.InputTokens != "" {
			inputTokens = gjson.Get(jsonStr, cfg.Input.InputTokens).Int()
		}

		// output tokens chunk
		if chunkType == cfg.Output.ChunkType && cfg.Output.OutputTokens != "" {
			outputTokens = gjson.Get(jsonStr, cfg.Output.OutputTokens).Int()
		}
	}
	if err := scanner.Err(); err != nil {
		// log or ignore — stream ended with error, use whatever tokens we have so far
		_ = err
	}

	return inputTokens, outputTokens
}

// parseLastStream handles OpenAI-compatible streaming where usage appears
// only in the last meaningful chunk before [DONE].
//
// APML config example:
//
//	streaming:
//	  mode: last
//	  input_tokens: usage.prompt_tokens
//	  output_tokens: usage.completion_tokens
func parseLastStream(r io.Reader, cfg *apml.StreamingConfig) (int64, int64) {
	var inputTokens, outputTokens int64

	scanner := bufio.NewScanner(r)
	for scanner.Scan() {
		line := scanner.Text()

		jsonStr, ok := extractSSEData(line)
		if !ok {
			continue
		}

		// In last mode, overwrite on every chunk that has usage fields.
		// The final non-[DONE] chunk with usage data wins.
		if cfg.InputTokens != "" {
			if v := gjson.Get(jsonStr, cfg.InputTokens); v.Exists() {
				inputTokens = v.Int()
			}
		}
		if cfg.OutputTokens != "" {
			if v := gjson.Get(jsonStr, cfg.OutputTokens); v.Exists() {
				outputTokens = v.Int()
			}
		}
	}
	if err := scanner.Err(); err != nil {
		// log or ignore — stream ended with error, use whatever tokens we have so far
		_ = err
	}

	return inputTokens, outputTokens
}

// extractSSEData strips the "data: " prefix from an SSE line and returns
// the JSON payload. Returns ("", false) for empty lines, comments, and [DONE].
func extractSSEData(line string) (string, bool) {
	if !strings.HasPrefix(line, "data: ") {
		return "", false
	}
	payload := strings.TrimPrefix(line, "data: ")
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "[DONE]" {
		return "", false
	}
	return payload, true
}
