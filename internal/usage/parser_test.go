package usage

import (
	"bytes"
	"testing"

	"github.com/RainbowCloudLabs/ai.local/internal/apml"
)

// --- 1. Test Standard One-Shot JSON Parsing ---
func TestParseStandardJSON(t *testing.T) {
	provider := &apml.ProviderConfig{
		Usage: &apml.UsageConfig{
			InputTokens:  "usage.prompt_tokens",
			OutputTokens: "usage.completion_tokens",
		},
	}

	mockResponse := []byte(`{
		"id": "chatcmpl-123",
		"object": "chat.completion",
		"usage": {
			"prompt_tokens": 15,
			"completion_tokens": 120
		}
	}`)

	input, output := ParseStandardJSON(mockResponse, provider)
	if input != 15 || output != 120 {
		t.Errorf("Standard JSON mapping failed. Expected (15, 120), got (%d, %d)", input, output)
	}
}

// --- 2. Test OpenAI/OpenRouter Style Streaming (Last Mode) ---
func TestParseStreamReader_LastMode(t *testing.T) {
	provider := &apml.ProviderConfig{
		Streaming: &apml.StreamingConfig{
			Mode:         "last",
			InputTokens:  "usage.prompt_tokens",
			OutputTokens: "usage.completion_tokens",
		},
	}

	// Simulating the exact OpenRouter chunk stream layout discovered yesterday
	streamData := "data: {\"choices\":[{\"delta\":{\"content\":\"Hello\"}}]}\n" +
		"data: {\"choices\":[{\"delta\":{\"content\":\" there\"}}]}\n" +
		"data: {\"choices\":[],\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":1138,\"total_tokens\":1149}}\n" +
		"data: [DONE]\n"

	reader := bytes.NewBufferString(streamData)
	input, output := ParseStreamReader(reader, provider)

	if input != 11 || output != 1138 {
		t.Errorf("Last mode streaming parsing failed. Expected (11, 1138), got (%d, %d)", input, output)
	}
}

// --- 3. Test Anthropic Style Streaming (Split Mode) ---
func TestParseStreamReader_SplitMode(t *testing.T) {
	provider := &apml.ProviderConfig{
		Streaming: &apml.StreamingConfig{
			Mode: "split",
			Input: &apml.EventConfig{
				ChunkType:   "message_start",
				InputTokens: "message.usage.input_tokens",
			},
			Output: &apml.EventConfig{
				ChunkType:    "message_delta",
				OutputTokens: "usage.output_tokens",
			},
		},
	}

	// Simulating multiple distinct chunks broadcasting down the pipeline
	streamData := "data: {\"type\":\"message_start\",\"message\":{\"usage\":{\"input_tokens\":45}}}\n" +
		"data: {\"type\":\"content_block_start\",\"index\":0}\n" +
		"data: {\"type\":\"message_delta\",\"usage\":{\"output_tokens\":256}}\n" +
		"data: [DONE]\n"

	reader := bytes.NewBufferString(streamData)
	input, output := ParseStreamReader(reader, provider)

	if input != 45 || output != 256 {
		t.Errorf("Split mode streaming parsing failed. Expected (45, 256), got (%d, %d)", input, output)
	}
}

// --- 4. Test Telemetry Blindspots (Zero Token Warning Boundaries) ---
func TestParseStreamReader_WarningBoundaries(t *testing.T) {
	provider := &apml.ProviderConfig{
		Streaming: &apml.StreamingConfig{
			Mode:         "last",
			InputTokens:  "usage.prompt_tokens",
			OutputTokens: "usage.completion_tokens",
		},
	}

	// Garbage data or broken schema fields injected by upstream provider
	brokenStream := "data: {\"type\":\"error_anomaly\",\"message\":\"corrupted fields\"}\n" +
		"data: [DONE]\n"

	reader := bytes.NewBufferString(brokenStream)
	input, output := ParseStreamReader(reader, provider)

	// The system must degrade gracefully to 0 tokens without crashing the execution ring
	if input != 0 || output != 0 {
		t.Errorf("Edge case guard faulted. Expected (0, 0) for anomaly data, got (%d, %d)", input, output)
	}
}
