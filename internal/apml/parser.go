package apml

import (
	"fmt"
	"os"
	"regexp"
	"strings"

	"gopkg.in/yaml.v3"
)

// alphaNumericIdentifierRegex enforces that identifiers (such as quotas and providers)
// must start with an ASCII letter, followed by any number of alphanumeric characters,
// underscores, or hyphens. This permits expressive naming conventions (e.g., "claude-3-5-sonnet", "vip_pool_2").
var alphaNameRegex = regexp.MustCompile(`^[a-zA-Z][a-zA-Z0-9_-]*$`)
var planVersionRegex = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9-]*$`)

// Parse reads, unmarshals, and validates an APML configuration file from the given path.
func Parse(path string) (*APMLConfig, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read apml file: %w", err)
	}

	var config APMLConfig
	if err := yaml.Unmarshal(data, &config); err != nil {
		return nil, fmt.Errorf("failed to parse apml file: %w", err)
	}

	if err := config.validate(); err != nil {
		return nil, fmt.Errorf("invalid apml config: %w", err)
	}

	return &config, nil
}

func (c *APMLConfig) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.MappingNode {
		return fmt.Errorf("apml config must be a mapping object")
	}

	c.Routes = make(map[string]RouteConfig)

	// Iterate through the top-level key-value pairs of the YAML document.
	for i := 0; i < len(value.Content); i += 2 {
		keyNode := value.Content[i]
		valNode := value.Content[i+1]

		if strings.HasPrefix(keyNode.Value, "/") {
			var route RouteConfig
			if err := valNode.Decode(&route); err != nil {
				return fmt.Errorf("failed to decode route %s: %w", keyNode.Value, err)
			}
			c.Routes[keyNode.Value] = route
			continue
		}

		// Condition B: Standard top-level fields and block metadata processing.
		switch keyNode.Value {
		case "title":
			c.Title = valNode.Value
		case "baseUri":
			c.BaseURI = valNode.Value
		case "version":
			c.Version = valNode.Value
		case "plan_version":
			c.PlanVersion = valNode.Value
		case "quotas":
			if err := valNode.Decode(&c.Quotas); err != nil {
				return fmt.Errorf("failed to decode quotas: %w", err)
			}
		case "providers":
			if err := valNode.Decode(&c.Providers); err != nil {
				return fmt.Errorf("failed to decode providers: %w", err)
			}
		}
	}

	return nil
}

// validate sanity checks required fields, naming constraints, and referential integrity.
func (c *APMLConfig) validate() error {
	// 1. Core Metadata Verification
	if c.Title == "" {
		return fmt.Errorf("title is required")
	}
	if c.BaseURI == "" {
		return fmt.Errorf("baseUri is required")
	}
	if c.Version == "" {
		return fmt.Errorf("version is required")
	}
	if c.PlanVersion == "" {
		return fmt.Errorf("plan_version is required")
	}
	if !planVersionRegex.MatchString(c.PlanVersion) {
		return fmt.Errorf("invalid plan_version name %q", c.PlanVersion)
	}
	if len(c.Routes) == 0 {
		return fmt.Errorf("at least one route is required")
	}

	// 2. Quota Block Verification and Content Auditing
	for name, q := range c.Quotas {
		if !alphaNameRegex.MatchString(name) {
			return fmt.Errorf(
				"invalid quota name %q: must contain only english letters [a-zA-Z]",
				name,
			)
		}
		if q.Daily < 0 || q.Monthly < 0 {
			return fmt.Errorf("quota %s cannot have negative limits", name)
		}

		// unlimited
		if q.Daily == 0 && q.Monthly == 0 {
			continue
		}

		// Enforce operational strategies for all active limiting quotas.
		if q.Mode != "per_key" && q.Mode != "shared" {
			return fmt.Errorf(
				"quota %s has invalid mode: %q (must be 'per_key' or 'shared' for active limits)",
				name,
				q.Mode,
			)
		}
	}

	// 3. Provider Block Verification and Structural Diagnostics
	for name, p := range c.Providers {
		if !alphaNameRegex.MatchString(name) {
			return fmt.Errorf(
				"invalid provider name %q: must contain only english letters [a-zA-Z]",
				name,
			)
		}
		if p.Host == "" {
			return fmt.Errorf("provider %s: host is required", name)
		}

		// Standard Usage Validation
		if p.Usage == nil {
			return fmt.Errorf("usage config of providers is nil")
		}
		if p.Usage.InputTokens == "" {
			return fmt.Errorf("provider %s: usage.input_tokens is required", name)
		}
		if p.Usage.OutputTokens == "" {
			return fmt.Errorf("provider %s: usage.output_tokens is required", name)
		}

		// Streaming Layer Validation
		if p.Streaming == nil {
			return fmt.Errorf("streaming config of providers is nil")
		}
		switch p.Streaming.Mode {
		case "split", "last":
			// Valid streaming strategies
		default:
			return fmt.Errorf("provider %s: unknown streaming mode %q", name, p.Streaming.Mode)
		}

		// Specific Validation for 'last' Mode Telemetry
		if p.Streaming.Mode == "last" {
			if p.Streaming.InputTokens == "" {
				return fmt.Errorf(
					"provider %s: streaming.input_tokens is required for last mode",
					name,
				)
			}
			if p.Streaming.OutputTokens == "" {
				return fmt.Errorf(
					"provider %s: streaming.output_tokens is required for last mode",
					name,
				)
			}
			if p.Streaming.Input != nil || p.Streaming.Output != nil {
				return fmt.Errorf(
					"provider %s: input and output blocks must be omitted when mode is 'last'",
					name,
				)
			}
		}

		// Specific Validation for 'split' Mode Telemetry (e.g., Anthropic)
		if p.Streaming.Mode == "split" {
			if p.Streaming.Input == nil || p.Streaming.Output == nil {
				return fmt.Errorf(
					"provider %s: split mode requires both input and output configuration objects",
					name,
				)
			}
			if p.Streaming.Input.ChunkType == "" || p.Streaming.Input.InputTokens == "" {
				return fmt.Errorf("provider %s: streaming.input block fields are incomplete", name)
			}
			if p.Streaming.Output.ChunkType == "" || p.Streaming.Output.OutputTokens == "" {
				return fmt.Errorf("provider %s: streaming.output block fields are incomplete", name)
			}
			if p.Streaming.InputTokens != "" || p.Streaming.OutputTokens != "" {
				return fmt.Errorf(
					"provider %s: top-level streaming.input_tokens and output_tokens must be omitted when mode is"+
						"'split' (use sub-blocks instead)",
					name,
				)
			}
		}
	}

	for path, route := range c.Routes {
		if route.Provider == "" {
			return fmt.Errorf("route %s: provider is required", path)
		}

		// Cross-reference: Verify targeted provider exists within the system definition.
		if _, ok := c.Providers[route.Provider]; !ok {
			return fmt.Errorf(
				"route %s: provider %q is not defined in providers",
				path,
				route.Provider,
			)
		}

		// Cross-reference: Verify targeted quota exists within the system definition if provided.
		if route.Quota != "" {
			if _, ok := c.Quotas[route.Quota]; !ok {
				return fmt.Errorf("route %s: quota %q is not defined in quotas", path, route.Quota)
			}
		}
	}

	return nil
}

// ResolveQuota safely performs a routing table lookup to resolve a route path to its QuotaDetail.
// Returns a zero-value struct and false if the path doesn't exist or doesn't bind to an active quota.
func (c *APMLConfig) ResolveQuota(routePath string) (QuotaDetail, bool) {
	route, ok := c.Routes[routePath]
	if !ok || route.Quota == "" {
		return QuotaDetail{}, false
	}
	quota, ok := c.Quotas[route.Quota]
	return quota, ok
}
