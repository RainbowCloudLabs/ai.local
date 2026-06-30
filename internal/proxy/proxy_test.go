package proxy

import (
	"database/sql"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/RainbowCloudLabs/ai.local/internal/apml"
	"github.com/RainbowCloudLabs/ai.local/internal/keystore"
	"github.com/RainbowCloudLabs/ai.local/internal/usage"
	_ "modernc.org/sqlite"
)

// closeNotifyRecorder wraps httptest.ResponseRecorder and implements
// http.CloseNotifier to satisfy Gin's internal type assertion.
// httptest.ResponseRecorder does not implement CloseNotifier by default,
// which causes a panic when Gin's responseWriter tries to cast it.
type closeNotifyRecorder struct {
	*httptest.ResponseRecorder
	closed chan bool
}

// newTestUsageBackend creates an in-memory SQLite-backed UsageBackend for tests.
// ":memory:" means no file is touched on disk, and each call gets a fresh DB.
func newTestUsageBackend(t *testing.T) (*usage.UsageBackend, *usage.UsageStore) {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := usage.InitSchema(db); err != nil {
		t.Fatalf("failed to init usage schema: %v", err)
	}

	backend := usage.NewUsageBackend(db)
	quota := usage.NewUsageStore(db)
	backend.StartWorker()
	t.Cleanup(backend.Stop)

	return backend, quota
}

func newRecorder() *closeNotifyRecorder {
	return &closeNotifyRecorder{
		ResponseRecorder: httptest.NewRecorder(),
		closed:           make(chan bool, 1),
	}
}

func (r *closeNotifyRecorder) CloseNotify() <-chan bool {
	return r.closed
}

// TestNewProxyServer_CreatesRoutes verifies that NewProxyServer reads the APML config
// and registers a Gin route for each provider (e.g. /claude/*any).
func TestNewProxyServer_CreatesRoutes(t *testing.T) {
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: "api.anthropic.com",
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	usageBackend, quota := newTestUsageBackend(t)
	server, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() unexpected error: %v", err)
	}
	if server == nil {
		t.Fatal("NewProxyServer() returned nil")
	}
	if server.engine == nil {
		t.Fatal("Gin engine is nil")
	}
}

// TestProxy_ForwardsRequestToUpstream verifies that a request to /claude/v1/messages
// is correctly forwarded to the upstream server.
//
// Flow:
//
//	client -> GET /claude/v1/messages
//	       -> ProxyServer strips /claude prefix
//	       -> forwards GET /v1/messages to upstream
//	       -> upstream responds 200
//	       -> client receives 200
func TestProxy_ForwardsRequestToUpstream(t *testing.T) {
	// 1. Start a fake upstream server that simulates api.anthropic.com
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("upstream received path %q, want %q", r.URL.Path, "/v1/messages")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "ok"}`))
	}))
	defer upstream.Close()

	// 2. Build config pointing /claude to our fake upstream
	upstreamHost := upstream.URL[len("http://"):]
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: upstreamHost,
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	key, keyErr := store.AddKey("/claude", "sk-c-abcdefg", "mock-key")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}
	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	// 3. Use closeNotifyRecorder instead of httptest.NewRecorder()
	w := newRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claude/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+key.InternalKey)

	proxyServer.engine.ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Errorf("response status = %d, want %d", w.Code, http.StatusOK)
	}
}

// TestProxy_MultipleRoutes verifies that multiple providers defined in APML
// are each correctly routed to their respective upstream servers.
func TestProxy_MultipleRoutes(t *testing.T) {
	claudeUpstream := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Served-By", "claude-fake")
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer claudeUpstream.Close()

	codexUpstream := httptest.NewServer(
		http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("X-Served-By", "codex-fake")
			w.WriteHeader(http.StatusOK)
		}),
	)
	defer codexUpstream.Close()

	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: claudeUpstream.URL[len("http://"):],
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
			"codex_mock": {
				Host: codexUpstream.URL[len("http://"):],
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
			"/codex": {
				Provider: "codex_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	claudeKey, claudeKeyErr := store.AddKey("/claude", "sk-calude-abcdefg", "mock-claude")
	if claudeKeyErr != nil {
		t.Fatalf("AddKey() error: %v", claudeKeyErr)
	}
	cadexKey, cadexKeyErr := store.AddKey("/codex", "sk-codex-abcdefg", "mock-codex")
	if cadexKeyErr != nil {
		t.Fatalf("AddKey() error: %v", cadexKeyErr)
	}

	w1 := newRecorder()
	req1 := httptest.NewRequest(http.MethodGet, "/claude/v1/messages", nil)
	req1.Header.Set("Authorization", "Bearer "+claudeKey.InternalKey)
	proxyServer.engine.ServeHTTP(w1, req1)
	if w1.Code != http.StatusOK {
		t.Errorf("/claude response = %d, want 200", w1.Code)
	}
	if w1.Header().Get("X-Served-By") != "claude-fake" {
		t.Errorf("/claude was not routed to claude upstream")
	}

	w2 := newRecorder()
	req2 := httptest.NewRequest(http.MethodGet, "/codex/v1/chat/completions", nil)
	req2.Header.Set("Authorization", "Bearer "+cadexKey.InternalKey)
	proxyServer.engine.ServeHTTP(w2, req2)
	if w2.Code != http.StatusOK {
		t.Errorf("/codex response = %d, want 200", w2.Code)
	}
	if w2.Header().Get("X-Served-By") != "codex-fake" {
		t.Errorf("/codex was not routed to codex upstream")
	}
}

// TestProxy_PathStripping verifies that the /claude prefix is stripped
// before forwarding to the upstream server.
//
// e.g. client sends:   GET /claude/v1/models
//
//	upstream sees:  GET /v1/models   (NOT /claude/v1/models)
func TestProxy_PathStripping(t *testing.T) {
	var receivedPath string

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedPath = r.URL.Path
		w.WriteHeader(http.StatusOK)
	}))
	defer upstream.Close()

	upstreamHost := upstream.URL[len("http://"):]
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: upstreamHost,
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, _ := NewProxyServer(cfg, store, usageBackend, quota)

	key, keyErr := store.AddKey("/claude", "sk-calude-abcdefg", "mock-claude")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}
	w := newRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claude/v1/models", nil)
	req.Header.Set("Authorization", "Bearer "+key.InternalKey)
	proxyServer.engine.ServeHTTP(w, req)

	if receivedPath != "/v1/models" {
		t.Errorf("upstream received path %q, want %q", receivedPath, "/v1/models")
	}
}

// TestProxy_UpstreamFailure verifies that when the upstream server is unreachable,
// the proxy returns HTTP 502 Bad Gateway instead of crashing.
func TestProxy_UpstreamFailure(t *testing.T) {
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: "127.0.0.1:19999",
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	usageBackend, quota := newTestUsageBackend(t)
	key, keyErr := store.AddKey("/claude", "sk-c-abcdefg", "mock-key")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	w := newRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claude/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+key.InternalKey)
	proxyServer.engine.ServeHTTP(w, req)

	if w.Code != http.StatusBadGateway {
		t.Errorf("response status = %d, want %d (Bad Gateway)", w.Code, http.StatusBadGateway)
	}
}

// TestProxy_LocalKeyNotMatch
func TestProxy_LocalKeyNotMatch(t *testing.T) {
	// 1. Start a fake upstream server that simulates api.anthropic.com
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/messages" {
			t.Errorf("upstream received path %q, want %q", r.URL.Path, "/v1/messages")
		}
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{"message": "ok"}`))
	}))
	defer upstream.Close()

	// 2. Build config pointing /claude to our fake upstream
	upstreamHost := upstream.URL[len("http://"):]
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host: upstreamHost,
				Usage: &apml.UsageConfig{
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "input_tokens",
					OutputTokens: "output_tokens",
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "",
			},
		},
	}

	store := keystore.NewStore()
	_, keyErr := store.AddKey("/claude", "sk-c-abcdefg", "mock-key")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}
	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	// 3. Use closeNotifyRecorder instead of httptest.NewRecorder()
	w := newRecorder()
	req := httptest.NewRequest(http.MethodGet, "/claude/v1/messages", nil)
	req.Header.Set("Authorization", "Bearer "+"sk-f-abcdefg")

	proxyServer.engine.ServeHTTP(w, req)

	if w.Code != http.StatusUnauthorized {
		t.Errorf("response status = %d, want %d", w.Code, http.StatusUnauthorized)
	}
}

// TestProxy_OpenAI_Streaming_Injection verifies that the gateway correctly interprets
// the request_option from the APML configuration and automatically sanitizes and injects
// stream_options: { "include_usage": true } into the inbound JSON body during streaming requests.
func TestProxy_OpenAIStreamingInjection(t *testing.T) {
	var receivedBody string

	// 1. Spin up a mock OpenAI upstream server to capture the sanitized body forwarded by the gateway.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		bodyBytes, err := io.ReadAll(r.Body)
		if err != nil {
			t.Errorf("failed to read upstream request body: %v", err)
		}
		receivedBody = string(bodyBytes)

		// Simulate a standard OpenAI Chunk stream response.
		w.Header().Set("Content-Type", "text/event-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("data: {\"choices\":[],\"usage\":{\"prompt_tokens\":10,\"completion_tokens\":20}}\n\n"))
	}))
	defer upstream.Close()

	// 2. Construct APML configuration with the openai route containing request_option.
	upstreamHost := upstream.URL[len("http://"):]
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"openai_mock": {
				Host: upstreamHost,
				Usage: &apml.UsageConfig{
					InputTokens:  "prompt_tokens",
					OutputTokens: "completion_tokens",
				},
				Streaming: &apml.StreamingConfig{
					Mode:         "last",
					InputTokens:  "prompt_tokens",
					OutputTokens: "completion_tokens",
					RequestOption: map[string]interface{}{
						"stream_options": map[string]interface{}{
							"include_usage": true,
						},
					},
				},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/openai": {
				Provider: "openai_mock",
				Quota:    "medium",
			},
		},
	}

	store := keystore.NewStore()
	key, keyErr := store.AddKey("/openai", "sk-o-abcdefg", "mock-openai")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}

	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	// 3. Simulate a client outbound streaming request that initially lacks stream_options.
	w := newRecorder()
	jsonPayload := `{"model":"gpt-4o","stream":true,"messages":[{"role":"user","content":"hi"}]}`
	req := httptest.NewRequest(http.MethodPost, "/openai/v1/chat/completions", strings.NewReader(jsonPayload))
	req.Header.Set("Authorization", "Bearer "+key.InternalKey)
	req.Header.Set("Content-Type", "application/json")

	proxyServer.engine.ServeHTTP(w, req)

	// 4. Core Assertion: Verify if the upstream actually received the payload injected with stream_options.
	if !strings.Contains(receivedBody, `"stream_options"`) || !strings.Contains(receivedBody, `"include_usage":true`) {
		t.Errorf("L7 inbound body rewrite failed! Upstream did not receive injected stream_options. Received: %s",
			receivedBody)
	}
}

// TestProxy_ClaudeAPIKeyMatch verifies that when a client uses a custom header (e.g., X-API-Key),
// the gateway successfully authenticates the request via dual-track auth and translates the
// local key into the correct real upstream key upon egress.
func TestProxy_ClaudeAPIKeyMatch(t *testing.T) {
	var receivedAuthHeader string

	// 1. Start a fake upstream server to capture the outbound authentication header rewritten by the gateway.
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		receivedAuthHeader = r.Header.Get("X-API-Key")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"message": "claude ok"}`))
	}))
	defer upstream.Close()

	// 2. Build APML config defining the custom api_key_prefix for Claude.
	upstreamHost := upstream.URL[len("http://"):]
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host:         upstreamHost,
				APIKeyPrefix: "X-API-Key",
				InputMessage: "messages.content",
				Usage:        &apml.UsageConfig{InputTokens: "in", OutputTokens: "out"},
				Streaming:    &apml.StreamingConfig{Mode: "split"},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "medium",
			},
		},
	}

	store := keystore.NewStore()
	// Add a key matching the route, establishing the mapping from sk-local-123 to real upstream sk-ant-real
	key, keyErr := store.AddKey("/claude", "sk-ant-real", "mock-claude")
	if keyErr != nil {
		t.Fatalf("AddKey() error: %v", keyErr)
	}

	usageBackend, quota := newTestUsageBackend(t)
	proxyServer, err := NewProxyServer(cfg, store, usageBackend, quota)
	if err != nil {
		t.Fatalf("NewProxyServer() error: %v", err)
	}

	// 3. Simulate an inbound request mimicking the official Anthropic SDK structure.
	w := newRecorder()
	req := httptest.NewRequest(http.MethodPost, "/claude/v1/messages", nil)
	// The SDK injects the local gateway token into X-API-Key rather than the Authorization header.
	req.Header.Set("X-API-Key", key.InternalKey)

	proxyServer.engine.ServeHTTP(w, req)

	// 4. Core Assertions
	if w.Code != http.StatusOK {
		t.Errorf("expected response status 200, got %d", w.Code)
	}
	if receivedAuthHeader != "sk-ant-real" {
		t.Errorf("Egress translation failed! Upstream received X-API-Key %q, want %q", receivedAuthHeader, "sk-ant-real")
	}
}

// TestProxy_ClaudeAPIKeyMismatch verifies the boundary security defensive behavior when
// the custom authentication header is either missing, malformed, or carries an invalid token.
func TestProxy_ClaudeAPIKeyMismatch(t *testing.T) {
	// 1. Build basic APML config configured with Claude's custom header semantics.
	cfg := &apml.APMLConfig{
		Title:   "test",
		BaseURI: "http://ai.local",
		Version: "1.0",
		Providers: map[string]apml.ProviderConfig{
			"anthropic_mock": {
				Host:         "127.0.0.1:8888", // Outbound transport safety fallback
				APIKeyPrefix: "X-API-Key",
				InputMessage: "messages.content",
				Usage:        &apml.UsageConfig{InputTokens: "in", OutputTokens: "out"},
				Streaming:    &apml.StreamingConfig{Mode: "split"},
			},
		},
		Routes: map[string]apml.RouteConfig{
			"/claude": {
				Provider: "anthropic_mock",
				Quota:    "medium",
			},
		},
	}

	store := keystore.NewStore()
	usageBackend, quota := newTestUsageBackend(t)
	_, _ = store.AddKey("/claude", "sk-ant-real", "mock-claude")
	proxyServer, _ := NewProxyServer(cfg, store, usageBackend, quota)

	// Scenario A: The custom header is completely missing from the client request.
	t.Run("Missing_Custom_Header", func(t *testing.T) {
		w := newRecorder()
		req := httptest.NewRequest(http.MethodPost, "/claude/v1/messages", nil)
		// No headers are added here

		proxyServer.engine.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for missing custom header, got %d", w.Code)
		}
	})

	// Scenario B: The custom header is structurally malformed (e.g., contains whitespace or multiple blocks).
	t.Run("Malformed_Custom_Header", func(t *testing.T) {
		w := newRecorder()
		req := httptest.NewRequest(http.MethodPost, "/claude/v1/messages", nil)
		// Custom tokens should be single fields, passing multiple values triggers structural errors.
		req.Header.Set("X-API-Key", "sk-local-123 dynamic-dirty-payload")

		proxyServer.engine.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for malformed custom header structural shape, got %d", w.Code)
		}
	})

	// Scenario C: The custom header exists but contains an unmapped/invalid gateway token.
	t.Run("Invalid_Custom_Token", func(t *testing.T) {
		w := newRecorder()
		req := httptest.NewRequest(http.MethodPost, "/claude/v1/messages", nil)
		// Providing a fully valid shape but utilizing a random fake internal token.
		req.Header.Set("X-API-Key", "sk-local-phantom-ghost-token")

		proxyServer.engine.ServeHTTP(w, req)

		if w.Code != http.StatusUnauthorized {
			t.Errorf("expected 401 Unauthorized for unauthorized invalid custom token value, got %d", w.Code)
		}
	})
}
