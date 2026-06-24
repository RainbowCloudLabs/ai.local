package proxy

import (
	"bytes"
	"fmt"
	"io"
	"net/http"
	"net/http/httputil"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/daneshih1125/ai.local/internal/apml"
	"github.com/daneshih1125/ai.local/internal/keystore"
	"github.com/daneshih1125/ai.local/internal/usage"
	"github.com/gin-gonic/gin"
	"github.com/tidwall/gjson"
	"github.com/tidwall/sjson"
)

// ProxyServer is the core L7 reverse proxy gateway driven by APML config.
type ProxyServer struct {
	cfg          *apml.APMLConfig
	store        *keystore.Store
	engine       *gin.Engine
	usageBackend *usage.UsageBackend
	usageStore   *usage.UsageStore
}

type autoCloseWriter struct {
	io.ReadCloser
	onClose func()
	once    sync.Once
}

func (a *autoCloseWriter) Close() error {
	a.once.Do(a.onClose)
	return a.ReadCloser.Close()
}

func buildTargetURL(baseURI, host string) (*url.URL, error) {
	if strings.HasPrefix(host, "http://") || strings.HasPrefix(host, "https://") {
		return url.Parse(host)
	}

	// Extract scheme from baseURI (defaults to https if extraction fails)
	scheme := "https://"
	if strings.HasPrefix(baseURI, "http://") {
		scheme = "http://"
	}

	return url.Parse(scheme + host)
}

// NewProxyServer initializes the proxy gateway from APML config.
func NewProxyServer(cfg *apml.APMLConfig, store *keystore.Store, usageBackend *usage.UsageBackend, usageStore *usage.UsageStore) (*ProxyServer, error) {
	gin.SetMode(gin.ReleaseMode)
	r := gin.New()
	r.Use(gin.Recovery())

	p := &ProxyServer{
		cfg:          cfg,
		store:        store,
		engine:       r,
		usageBackend: usageBackend,
		usageStore:   usageStore,
	}

	if err := p.setupRoutes(); err != nil {
		return nil, err
	}

	return p, nil
}

// setupRoutes registers a wildcard route for each provider defined in APML.
// e.g. /claude/*any -> api.anthropic.com
func (p *ProxyServer) setupRoutes() error {
	for path, route := range p.cfg.Routes {
		provider, _ := p.cfg.Providers[route.Provider]
		targetURL, err := buildTargetURL(p.cfg.BaseURI, provider.Host)
		if err != nil {
			return fmt.Errorf("route %s: invalid host %q: %w", path, provider.Host, err)
		}

		// capture loop variables for closure
		routePath := path
		target := targetURL
		routeCfg := route

		p.engine.Any(routePath+"/*any", p.authInterceptor(routePath), func(c *gin.Context) {
			p.handleReverseProxy(c, routePath, target, routeCfg)
		})
	}
	return nil
}

func (p *ProxyServer) authInterceptor(routePath string) gin.HandlerFunc {
	return func(c *gin.Context) {
		routeCfg := p.cfg.Routes[routePath]
		providerCfg, exists := p.cfg.Providers[routeCfg.Provider]

		authHeaderKey := "Authorization"
		isCustomHeader := false
		clientIP := c.ClientIP()

		if exists && providerCfg.APIKeyPrefix != "" {
			if !strings.HasPrefix(strings.ToLower(providerCfg.APIKeyPrefix), "authorization:") {
				authHeaderKey = providerCfg.APIKeyPrefix
				isCustomHeader = true
			}
		}

		authHeader := c.GetHeader(authHeaderKey)
		parts := strings.Fields(authHeader)

		var localKey string

		if isCustomHeader {
			if len(parts) != 1 {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":  "Unauthorized",
					"detail": fmt.Sprintf("Missing or malformed key in custom header '%s'.", authHeaderKey),
				})
				return
			}
			localKey = parts[0]
		} else {
			if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
				c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
					"error":  "Unauthorized",
					"detail": "Missing or malformed Authorization header. Expected 'Bearer <key>'.",
				})
				return
			}
			localKey = parts[1]
		}

		keyRecord, exists := p.store.GetKeyByInternal(localKey)
		if !exists || keyRecord.Route != routePath {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{
				"error":  "Unauthorized",
				"detail": "Invalid local API key or mismatched route gateway definition.",
			})
			return
		}

		// resolve quota
		quotaName := p.cfg.Routes[routePath].Quota
		if quotaName != "" {
			quota := p.cfg.Quotas[quotaName]
			bodyBytes, _ := io.ReadAll(c.Request.Body)
			c.Request.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

			result := p.usageStore.QuotaCheck(localKey, routePath, quota, bodyBytes, &providerCfg)
			if !result.Allowed {
				c.AbortWithStatusJSON(http.StatusTooManyRequests, gin.H{
					"error":  "quota exceeded",
					"detail": result.DenyReason,
				})
				return
			}
		}

		c.Set("realKey", keyRecord.RealKey)
		c.Set("localKey", localKey)
		c.Set("clientIP", clientIP)
		c.Next()
	}
}

// executeMergeOptionsInline refactors sjson injection to operate on hot memory slices directly,
// eliminating the need to trigger redundant io.ReadAll streams on r.In.Body.
func (p *ProxyServer) executeMergeOptionsInline(bodyBytes []byte, apmlOpts map[string]interface{}) []byte {
	// 1. Enforce stream: true parameter if missing to fulfill core billing data constraints.
	// We force this at the doorstep of the inline injector since we are already locked into isStreaming.
	if !gjson.GetBytes(bodyBytes, "stream").Exists() {
		bodyBytes, _ = sjson.SetBytes(bodyBytes, "stream", true)
	}

	// 2. Optimization: Flatten the nested map into sjson-compatible dot paths.
	// This flattens {"stream_options": {"include_usage": true}} into a single pass execution.
	for topKey, topVal := range apmlOpts {
		if subMap, ok := topVal.(map[string]interface{}); ok {
			for subKey, subVal := range subMap {
				// Construct dot notation path, e.g., "stream_options.include_usage"
				jsonPath := topKey + "." + subKey
				bodyBytes, _ = sjson.SetBytes(bodyBytes, jsonPath, subVal)
			}
		} else {
			// Fallback for flat top-level configurations
			bodyBytes, _ = sjson.SetBytes(bodyBytes, topKey, topVal)
		}
	}

	// 3. Return the sanitized hot memory slice directly back to the pipeline runner.
	return bodyBytes
}

// handleReverseProxy handles a single proxied request for a given provider route.
func (p *ProxyServer) handleReverseProxy(
	c *gin.Context,
	routePath string,
	target *url.URL,
	routeCfg apml.RouteConfig,
) {
	realKey := c.GetString("realKey")
	localKey := c.GetString("localKey")
	clientIP := c.GetString("clientIP")
	providerCfg, ok := p.cfg.Providers[routeCfg.Provider]
	if !ok {
		panic(fmt.Sprintf("APML Integrity Violation: provider %q bypassed boot-time validation", routeCfg.Provider))
	}

	proxy := &httputil.ReverseProxy{
		FlushInterval: 100 * time.Millisecond,
		Rewrite: func(r *httputil.ProxyRequest) {

			for key, values := range r.In.Header {
				if strings.EqualFold(key, "Authorization") {
					continue
				}
				if providerCfg.APIKeyPrefix != "" && strings.EqualFold(key, providerCfg.APIKeyPrefix) {
					continue
				}
				for _, val := range values {
					r.Out.Header.Add(key, val)
				}
			}

			if providerCfg.APIKeyPrefix != "" {
				r.Out.Header.Set(providerCfg.APIKeyPrefix, realKey)
			} else {
				r.Out.Header.Set("Authorization", "Bearer "+realKey)
			}

			r.SetURL(target)
			r.Out.Host = target.Host
			r.Out.URL.Path = c.Param("any")
			if r.Out.URL.Path == "" {
				r.Out.URL.Path = "/"
			}

			isStreaming := false
			if r.In.Body != nil && r.In.Body != http.NoBody {
				bodyBytes, err := io.ReadAll(r.In.Body)
				if err == nil {
					// 1. Evaluate the precise streaming footprint of the client request
					streamField := gjson.GetBytes(bodyBytes, "stream")
					if streamField.Exists() && streamField.Bool() {
						isStreaming = true
					}
					c.Set("streaming", isStreaming)

					// 2. Branching execution layer based on the resolved topology
					if isStreaming && providerCfg.Streaming != nil {
						// Invoke options injection using the already loaded binary context
						bodyBytes = p.executeMergeOptionsInline(bodyBytes, providerCfg.Streaming.RequestOption)
					}

					r.Out.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))
					r.Out.ContentLength = int64(len(bodyBytes))
					r.Out.Header.Set("Content-Length", strconv.Itoa(len(bodyBytes)))
				}
			}
		},
		ModifyResponse: func(res *http.Response) error {
			// Fail-safe safety valve: Skip telemetry tracking if the upstream transaction faulted
			if res.StatusCode != http.StatusOK {
				return nil
			}

			// Retrieve runtime session states captured during the ingress pipeline phase
			isStreaming := c.GetBool("streaming")

			// ─── Scenario A: Standard One-Shot JSON Response (Non-Streaming) ───
			if !isStreaming {
				bodyBytes, err := io.ReadAll(res.Body)
				if err != nil {
					return nil
				}
				res.Body = io.NopCloser(bytes.NewBuffer(bodyBytes))

				// Parse the complete vendor-specific payload once the transmission stream finishes
				promptTokens, completionTokens := usage.ParseStandardJSON(bodyBytes, &providerCfg)

				record := &usage.Record{
					LocalKey:         localKey,
					RoutePath:        routePath,
					ClientIP:         clientIP,
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      promptTokens + completionTokens,
					CreatedAt:        time.Now(),
				}
				p.usageBackend.EmitNonBlocking(record)
				return nil
			}

			// ─── Scenario B: Server-Sent Events (Streaming) ───
			// Provision an in-memory pipe connection topology for low-overhead chunk scraping
			pr, pw := io.Pipe()
			// Intercept outbound responses: as Gin consumes chunks, they replicate concurrently to PipeWriter (pw)
			//res.Body = io.NopCloser(io.TeeReader(res.Body, pw))
			res.Body = &autoCloseWriter{
				ReadCloser: io.NopCloser(io.TeeReader(res.Body, pw)),
				onClose:    func() { pw.Close() },
			}

			// Asynchronous scraper routine decoupling main proxy loop execution context to keep O(1) latency profile
			go func() {
				// Enforce cleanup on teardown boundaries to prevent descriptor leaks
				defer pr.Close()
				// Stream the pipe chunks into the vendor-aware buffer reader parser in real-time
				promptTokens, completionTokens := usage.ParseStreamReader(pr, &providerCfg)
				record := &usage.Record{
					LocalKey:         localKey,
					RoutePath:        routePath,
					ClientIP:         clientIP,
					PromptTokens:     promptTokens,
					CompletionTokens: completionTokens,
					TotalTokens:      promptTokens + completionTokens,
					CreatedAt:        time.Now(),
				}
				// Emit transactional metric record block into the backend channel reservoir
				p.usageBackend.EmitNonBlocking(record)
			}()

			return nil
		},
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			c.JSON(http.StatusBadGateway, gin.H{
				"error":  "upstream request failed",
				"detail": err.Error(),
				"route":  routePath,
			})
		},
	}
	proxy.ServeHTTP(c.Writer, c.Request)
}

// Start boots the proxy server over TLS.
func (p *ProxyServer) Start(addr, certPath, keyPath string) error {
	fmt.Printf("ai.local listening on https://0.0.0.0%s\n", addr)
	for path, route := range p.cfg.Routes {
		provider, _ := p.cfg.Providers[route.Provider]
		fmt.Printf("  %s -> https://%s\n", path, provider.Host)
	}
	return p.engine.RunTLS(addr, certPath, keyPath)
}
