package apml

import (
	//"os"
	"path/filepath"
	"strings"
	"testing"

	"gopkg.in/yaml.v3"
)

// helpers
func mustParse(t *testing.T, filename string) *APMLConfig {
	t.Helper()
	cfg, err := Parse(filepath.Join("testdata", filename))
	if err != nil {
		t.Fatalf("Parse(%q) unexpected error: %v", filename, err)
	}
	return cfg
}

func TestParse_APML_Suite(t *testing.T) {
	cfg := mustParse(t, "ai.local.apml")

	t.Run("TopLevelFields", func(t *testing.T) {
		if cfg.Title != "AI Gateway Plan" {
			t.Errorf("Title = %q, want %q", cfg.Title, "AI Gateway Plan")
		}
		if cfg.BaseURI != "https://ai.local" {
			t.Errorf("BaseURI = %q, want %q", cfg.BaseURI, "https://ai.local")
		}
		if cfg.Version != "draft" {
			t.Errorf("Version = %q, want %q", cfg.Version, "draft")
		}
		if cfg.PlanVersion != "2026Q3" {
			t.Errorf("Version = %q, want %q", cfg.Version, "2026Q3")
		}
	})

	t.Run("NumbersOfMaps", func(t *testing.T) {
		if len(cfg.Quotas) != 4 {
			t.Errorf("len(Quotas) = %d, want 4", len(cfg.Quotas))
		}
		if len(cfg.Providers) != 4 {
			t.Errorf("len(Providers) = %d, want 4", len(cfg.Providers))
		}
		if len(cfg.Routes) != 4 {
			t.Errorf("len(Routes) = %d, want 4", len(cfg.Routes))
		}
	})

	t.Run("QuotasFields", func(t *testing.T) {
		q0, ok := cfg.Quotas["unlimited"]
		if !ok {
			t.Fatalf("quota 'unlimited' not found")
		}
		if q0.Monthly != 0 || q0.Daily != 0 {
			t.Fatalf("incorrect tokens of quota 'unlimited'")
		}

		quotaSmall, ok := cfg.Quotas["small"]
		if !ok {
			t.Fatalf("quota 'small' not found")
		}
		if quotaSmall.Monthly != 100000 || quotaSmall.Daily != 10000 {
			t.Fatalf("incorrect tokens of quota 'small'")
		}
		if quotaSmall.Mode != "per_key" {
			t.Fatalf("incorrect mode of quota 'small'")
		}
	})

	t.Run("ProvidesFields", func(t *testing.T) {
		p, ok := cfg.Providers["claude"]
		if !ok {
			t.Fatalf("provider 'claude' not found")
		}
		if p.Host != "api.anthropic.com" {
			t.Fatalf("host = %v, expected api.anthropic.com", p.Host)
		}

		if p.APIKeyPrefix != "X-API-Key" {
			t.Fatalf("host = %v, expected X-API-Key", p.APIKeyPrefix)
		}

		if p.InputMessage != "messages.content" {
			t.Fatalf("host = %v, expected messages.content", p.InputMessage)
		}

		usage := p.Usage
		if usage.InputTokens != "usage.input_tokens" {
			t.Fatalf("input_tokens = %v, expected usage.input_tokens", usage.InputTokens)
		}
		if usage.OutputTokens != "usage.output_tokens" {
			t.Fatalf("output_tokens = %v, expected usage.output_tokens", usage.OutputTokens)
		}

		s := p.Streaming
		if s.Mode != "split" {
			t.Fatalf("Streaming mode = %v, expected split", s.Mode)
		}
		if s.Input.ChunkType != "message_start" {
			t.Fatalf("Input.ChunkType = %v, expected message_start", s.Input.ChunkType)

		}
		if s.Input.InputTokens != "message.usage.input_tokens" {
			t.Fatalf("Input.InputTokens = %v, expected message.usage.input_tokens", s.Input.InputTokens)

		}
	})

	// Core Validation: Verify that OpenAI's request_option block is successfully unmarshaled.
	t.Run("OpenAI_RequestOptions", func(t *testing.T) {
		p, ok := cfg.Providers["openai"]
		if !ok {
			t.Fatalf("provider 'openai' not found")
		}
		if p.Streaming == nil {
			t.Fatalf("openai streaming config is nil")
		}
		if p.Streaming.Mode != "last" {
			t.Fatalf("openai streaming mode = %v, expected 'last'", p.Streaming.Mode)
		}

		// Assert that the top-level RequestOption mapping exists.
		opts := p.Streaming.RequestOption
		if opts == nil {
			t.Fatalf("openai streaming.request_option is nil (failed to parse dynamically)")
		}

		// 2. Deeply validate the nested 'stream_options' block.
		streamOptsVal, ok := opts["stream_options"]
		if !ok {
			t.Fatalf("missing 'stream_options' key in request_option")
		}

		// Assert that yaml.v3 successfully restored this nested structure as a map.
		streamOpts, ok := streamOptsVal.(map[string]interface{})
		if !ok {
			t.Fatalf("'stream_options' is not a valid YAML mapping object")
		}

		// 3. Validate the leaf node 'include_usage' field.
		includeUsageVal, ok := streamOpts["include_usage"]
		if !ok {
			t.Fatalf("missing 'include_usage' inside stream_options")
		}

		includeUsage, ok := includeUsageVal.(bool)
		if !ok {
			t.Fatalf("'include_usage' value is not a boolean")
		}

		if !includeUsage {
			t.Errorf("include_usage = %v, want true", includeUsage)
		}
	})
}

func TestParse_SadPath_Suite(t *testing.T) {
	tests := []struct {
		name          string
		yamlContent   string
		expectedMatch string
	}{
		// =================================================================
		// 1. Core Metadata Verification & File I/O
		// =================================================================
		{
			name:          "Missing_Title",
			yamlContent:   "baseUri: https://ai.local\nversion: '1.0'\nplan_version: 2026\n/test: { provider: claude }",
			expectedMatch: "title is required",
		},
		{
			name:          "Missing_BaseURI",
			yamlContent:   "title: Gateway\nversion: '1.0'\nplan_version: 2026\n/test: { provider: claude }",
			expectedMatch: "baseUri is required",
		},
		{
			name:          "Missing_Version",
			yamlContent:   "title: Gateway\nbaseUri: https://ai.local\nplan_version: 2026\ntest: { provider: claude }",
			expectedMatch: "version is required",
		},
		{
			name:          "Missing_PlanVersion",
			yamlContent:   "title: Gateway\nbaseUri: https://ai.local\nversion: '1.0'\n/test: {provider: claude}",
			expectedMatch: "plan_version is required",
		},
		{
			name:          "Missing_Routes",
			yamlContent:   "title: Gateway\nbaseUri: https://ai.local\nplan_version: 2026\nversion: '1.0'",
			expectedMatch: "at least one route is required",
		},

		// =================================================================
		// 2. Quota Block Verification
		// =================================================================
		{
			name: "Invalid_Quota_Name",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
quotas:
  123small: { daily: 10, monthly: 100 }
/test: { provider: claude }`,
			expectedMatch: "invalid quota name",
		},
		{
			name: "Negative_Quota_Limit",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
quotas:
  bad: { daily: -5, monthly: 100 }
/test: { provider: claude }`,
			expectedMatch: "cannot have negative limits",
		},
		{
			name: "Invalid_Quota_Mode",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
quotas:
  active: { daily: 10, monthly: 100, mode: custom_mode }
/test: { provider: claude }`,
			expectedMatch: "has invalid mode",
		},

		// =================================================================
		// 3. Provider Block Verification
		// =================================================================
		{
			name: "Invalid_Provider_Name",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude.2: { host: api.com }
/test: { provider: claude }`,
			expectedMatch: "invalid provider name",
		},
		{
			name: "Provider_Missing_Host",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude: { host: "" }
/test: { provider: claude }`,
			expectedMatch: "host is required",
		},
		{
			name: "Provider_Missing_Usage_Block",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude: { host: api.com }
/test: { provider: claude }`,
			expectedMatch: "usage config of providers is nil",
		},
		{
			name: "Provider_Usage_Missing_InputTokens",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { path: usage, output_tokens: out }
/test: { provider: claude }`,
			expectedMatch: "usage.input_tokens is required",
		},
		{
			name: "Provider_Usage_Missing_OutputTokens",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { path: usage, input_tokens: in }
/test: { provider: claude }`,
			expectedMatch: "usage.output_tokens is required",
		},
		{
			name: "Provider_Missing_Streaming_Block",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { path: usage, input_tokens: in, output_tokens: out }
/test: { provider: claude }`,
			expectedMatch: "streaming config of providers is nil",
		},
		{
			name: "Provider_Unknown_Streaming_Mode",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { path: usage, input_tokens: in, output_tokens: out }
    streaming: { mode: single_packet }
/test: { provider: claude }`,
			expectedMatch: "unknown streaming mode",
		},

		// =================================================================
		// 3a. 'last' Mode Specific Verification
		// =================================================================
		{
			name: "Last_Mode_Missing_InputTokens",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  gemini:
    host: api.com
    usage: { path: usage, input_tokens: in, output_tokens: out }
    streaming: { mode: last, output_tokens: out }
/test: { provider: gemini }`,
			expectedMatch: "streaming.input_tokens is required for last mode",
		},
		{
			name: "Last_Mode_Missing_OutputTokens",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  gemini:
    host: api.com
    usage: { path: usage, input_tokens: in, output_tokens: out }
    streaming: { mode: last, input_tokens: in }
/test: { provider: gemini }`,
			expectedMatch: "streaming.output_tokens is required for last mode",
		},
		{
			name: "Last_Mode_With_Dirty_SubBlocks",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  gemini:
    host: api.com
    usage: { path: usage, input_tokens: in, output_tokens: out }
    streaming:
      mode: last
      input_tokens: in
      output_tokens: out
      input: { chunk_type: start, path: p, input_tokens: in } # Dirty sub-block!
/test: { provider: gemini }`,
			expectedMatch: "input and output blocks must be omitted when mode is 'last'",
		},

		// =================================================================
		// 3b. 'split' Mode Specific Verification
		// =================================================================
		{
			name: "Split_Mode_Missing_SubBlocks",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { input_tokens: usage.in, output_tokens: usage.out }
    streaming: { mode: split }
/test: { provider: claude }`,
			expectedMatch: "split mode requires both input and output configuration objects",
		},
		{
			name: "Split_Mode_Incomplete_Input_SubBlock",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { input_tokens: usage.in, output_tokens: usage.out }
    streaming:
      mode: split
      input: { chunk_type: start }
      output: { chunk_type: delta, output_tokens: p.out }
/test: { provider: claude }`,
			expectedMatch: "streaming.input block fields are incomplete",
		},
		{
			name: "Split_Mode_Incomplete_Output_SubBlock",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { input_tokens: usage.in, output_tokens: usage.out }
    streaming:
      mode: split
      input: { chunk_type: start, input_tokens: p.in }
      output: { chunk_type: delta }
/test: { provider: claude }`,
			expectedMatch: "streaming.output block fields are incomplete",
		},
		{
			name: "Split_Mode_With_Dirty_TopLevel_Tokens",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { input_tokens: u.in, output_tokens: u.out }
    streaming:
      mode: split
      input_tokens: dirty_token
      input: { chunk_type: start, input_tokens: p.in }
      output: { chunk_type: delta, output_tokens: p.out }
/test: { provider: claude }`,
			expectedMatch: "top-level streaming.input_tokens and output_tokens must be omitted",
		},

		// =================================================================
		// 4. L7 Routing Table Cross-Reference
		// =================================================================
		{
			name: "Route_Missing_Provider_Field",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
/test: { quota: small }`,
			expectedMatch: "provider is required",
		},
		{
			name: "Route_Points_To_Undefined_Provider",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
/test: { provider: phantom }`,
			expectedMatch: "is not defined in providers",
		},
		{
			name: "Route_Points_To_Undefined_Quota",
			yamlContent: `
title: Gateway
baseUri: https://ai.local
version: draft
plan_version: 2026-dev
providers:
  claude:
    host: api.com
    usage: { input_tokens: usage.in, output_tokens: usage.out }
    streaming: { mode: last, input_tokens: in, output_tokens: out }
/test: { provider: claude, quota: ghost_quota }`,
			expectedMatch: "is not defined in quotas",
		},

		// =================================================================
		// 5. UnmarshalYAML Syntactic Violations
		// =================================================================
		{
			name:          "Unmarshal_Not_A_Mapping_Object",
			yamlContent:   "- item1\n- item2", // YAML sequence instead of map
			expectedMatch: "apml config must be a mapping object",
		},
		{
			name:          "Unmarshal_Malformed_YAML_Syntax",
			yamlContent:   "title: : mapping error:",
			expectedMatch: "yaml:", // Should trigger structural parser error from package yaml.v3
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var config APMLConfig
			// Directly invoking UnmarshalYAML to test core parsing & structural errors
			err := yaml.Unmarshal([]byte(tt.yamlContent), &config)

			// If it clears Unmarshal, run explicit validate() to test business constraint errors
			if err == nil {
				err = config.validate()
			}

			// Guard A: Verifying that an exception is absolutely raised
			if err == nil {
				t.Fatalf("expected error containing %q, but got nil (security bypass!)", tt.expectedMatch)
			}

			// Guard B: Error message alignment check
			if !strings.Contains(err.Error(), tt.expectedMatch) {
				t.Errorf("error = %q, want key phrase %q", err.Error(), tt.expectedMatch)
			}
		})
	}
}

// TestParse_FileNotFound tests the I/O error branch inside the top-level Parse function.
func TestParse_FileNotFound(t *testing.T) {
	_, err := Parse("testdata/non_existent_file.apml")
	if err == nil {
		t.Fatal("expected error for missing file, got nil")
	}
	if !strings.Contains(err.Error(), "failed to read apml file") {
		t.Errorf("unexpected error message: %v", err)
	}
}
