package usage

import (
	"testing"
	"time"

	"github.com/daneshih1125/ai.local/internal/apml"
)

// newQuotaTestDB creates an isolated in-memory UsageStore for unit tests.
func newQuotaTestDB(t *testing.T) *UsageStore {
	t.Helper()
	db := newTestDB(t) // reuse worker_test.go helper
	return NewUsageStore(db)
}

// seedDaily inserts or increments a daily_usages row for test setup.
func seedDaily(t *testing.T, q *UsageStore, localKey, routePath, date string, tokens int64) {
	t.Helper()
	_, err := q.db.Exec(
		`INSERT INTO daily_usages (local_key, route_path, usage_date, daily_tokens)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(local_key, route_path, usage_date)
		 DO UPDATE SET daily_tokens = daily_tokens + excluded.daily_tokens`,
		localKey, routePath, date, tokens,
	)
	if err != nil {
		t.Fatalf("seedDaily error: %v", err)
	}
}

// seedMonthly inserts or increments a monthly_usages row for test setup.
func seedMonthly(t *testing.T, q *UsageStore, localKey, routePath, month string, tokens int64) {
	t.Helper()
	_, err := q.db.Exec(
		`INSERT INTO monthly_usages (local_key, route_path, usage_month, monthly_tokens)
		 VALUES (?, ?, ?, ?)
		 ON CONFLICT(local_key, route_path, usage_month)
		 DO UPDATE SET monthly_tokens = monthly_tokens + excluded.monthly_tokens`,
		localKey, routePath, month, tokens,
	)
	if err != nil {
		t.Fatalf("seedMonthly error: %v", err)
	}
}

// seedTestData inserts fixed records through triple-write path.
func seedTestData(t *testing.T, b *UsageBackend) {
	t.Helper()

	records := []struct {
		key       string
		route     string
		ip        string
		prompt    int64
		comp      int64
		total     int64
		createdAt string
	}{
		{"sk-local-01", "/v1/chat", "127.0.0.1", 50, 500, 550, "2026-05-15 14:00:00"},
		{"sk-local-01", "/v1/chat", "127.0.0.1", 10, 100, 110, "2026-06-20 10:00:00"},
		{"sk-local-02", "/v1/chat", "192.168.1.1", 20, 200, 220, "2026-06-20 15:30:00"},
		{"sk-local-01", "/v1/embeddings", "127.0.0.1", 5, 0, 5, "2026-06-21 09:15:00"},
	}

	for _, r := range records {
		ts, err := time.ParseInLocation("2006-01-02 15:04:05", r.createdAt, time.Local)
		if err != nil {
			t.Fatalf("parse createdAt %q: %v", r.createdAt, err)
		}

		rec := &Record{
			LocalKey:         r.key,
			RoutePath:        r.route,
			ClientIP:         r.ip,
			PromptTokens:     r.prompt,
			CompletionTokens: r.comp,
			TotalTokens:      r.total,
			CreatedAt:        ts,
		}
		if err := b.executeTripleWrite(rec); err != nil {
			t.Fatalf("executeTripleWrite failed: %v", err)
		}
	}
}

// makeQuota builds a QuotaDetail fixture with explicit mode and limits.
func makeQuota(mode string, daily, monthly int64) apml.QuotaDetail {
	return apml.QuotaDetail{Mode: mode, Daily: daily, Monthly: monthly}
}

// makeProvider builds a ProviderConfig fixture for token estimation tests.
func makeProvider(inputMessage string) *apml.ProviderConfig {
	return &apml.ProviderConfig{InputMessage: inputMessage}
}

// TestCheck_UnlimitedQuota_AlwaysAllowed verifies unlimited quota never blocks requests.
func TestCheck_UnlimitedQuota_AlwaysAllowed(t *testing.T) {
	q := newQuotaTestDB(t)
	quota := makeQuota("per_key", 0, 0)

	result := q.QuotaCheck("sk-local-abc", "/claude", quota, nil, makeProvider("messages"))
	if !result.Allowed {
		t.Errorf("unlimited quota should always allow, got denied: %s", result.DenyReason)
	}
}

// TestCheck_PerKey_Daily_AllowedWhenUnderLimit verifies per-key daily quota allows under-limit usage.
func TestCheck_PerKey_Daily_AllowedWhenUnderLimit(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")
	seedDaily(t, q, "sk-local-abc", "/claude", today, 5000)

	quota := makeQuota("per_key", 10000, 0)
	result := q.QuotaCheck("sk-local-abc", "/claude", quota, nil, makeProvider(""))
	if !result.Allowed {
		t.Errorf("expected allowed, got denied: %s", result.DenyReason)
	}
}

// TestCheck_PerKey_Daily_DeniedWhenOverLimit verifies per-key daily quota denies over-limit usage.
func TestCheck_PerKey_Daily_DeniedWhenOverLimit(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")
	seedDaily(t, q, "sk-local-abc", "/claude", today, 9999)

	quota := makeQuota("per_key", 10000, 0)
	body := []byte(`{"messages":[{"role":"user","content":"hello world test message here"}]}`)
	result := q.QuotaCheck("sk-local-abc", "/claude", quota, body, makeProvider("messages"))
	if result.Allowed {
		t.Error("expected denied when current + estimated > daily limit")
	}
	if result.QuotaLimit != 10000 {
		t.Errorf("QuotaLimit = %d, want 10000", result.QuotaLimit)
	}
}

// TestCheck_PerKey_Daily_AllowedWhenNoUsageYet verifies a new key with no usage is allowed.
func TestCheck_PerKey_Daily_AllowedWhenNoUsageYet(t *testing.T) {
	q := newQuotaTestDB(t)

	quota := makeQuota("per_key", 10000, 0)
	result := q.QuotaCheck("sk-local-new", "/claude", quota, nil, makeProvider(""))
	if !result.Allowed {
		t.Errorf("new key with no usage should be allowed, got: %s", result.DenyReason)
	}
}

// TestCheck_PerKey_Daily_IsolatedPerKey verifies one key's usage does not affect another key in per-key mode.
func TestCheck_PerKey_Daily_IsolatedPerKey(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedDaily(t, q, "key-A", "/claude", today, 9999)
	quota := makeQuota("per_key", 10000, 0)

	result := q.QuotaCheck("key-B", "/claude", quota, nil, makeProvider(""))
	if !result.Allowed {
		t.Errorf("key-B should be unaffected by key-A usage, got denied: %s", result.DenyReason)
	}
}

// TestCheck_PerKey_Monthly_DeniedWhenOverLimit verifies per-key monthly quota denies over-limit usage.
func TestCheck_PerKey_Monthly_DeniedWhenOverLimit(t *testing.T) {
	q := newQuotaTestDB(t)
	thisMonth := time.Now().Format("2006-01")
	seedMonthly(t, q, "sk-local-abc", "/claude", thisMonth, 99990)

	quota := makeQuota("per_key", 0, 100000)
	body := []byte(`{"messages":[{"role":"user","content":"hello world test message here longer text"}]}`)
	result := q.QuotaCheck("sk-local-abc", "/claude", quota, body, makeProvider("messages"))
	if result.Allowed {
		t.Error("expected denied when current + estimated > monthly limit")
	}
}

// TestCheck_PerKey_BothDailyAndMonthly_DailyBlocksFirst verifies daily limit is enforced before monthly limit.
func TestCheck_PerKey_BothDailyAndMonthly_DailyBlocksFirst(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")
	thisMonth := time.Now().Format("2006-01")

	seedDaily(t, q, "sk-local-abc", "/claude", today, 9999)
	seedMonthly(t, q, "sk-local-abc", "/claude", thisMonth, 100)

	quota := makeQuota("per_key", 10000, 100000)
	body := []byte(`{"messages":[{"role":"user","content":"hello world test message here"}]}`)
	result := q.QuotaCheck("sk-local-abc", "/claude", quota, body, makeProvider("messages"))
	if result.Allowed {
		t.Error("expected denied by daily limit")
	}
	if result.QuotaLimit != 10000 {
		t.Errorf("should be blocked by daily limit (10000), got QuotaLimit=%d", result.QuotaLimit)
	}
}

// TestCheck_Shared_Daily_SumsAllKeys verifies shared daily quota sums usage across keys on the same route.
func TestCheck_Shared_Daily_SumsAllKeys(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedDaily(t, q, "key-A", "/claude", today, 6000)
	seedDaily(t, q, "key-B", "/claude", today, 4000)

	quota := makeQuota("shared", 10000, 0)
	body := []byte(`{"messages":[{"role":"user","content":"hello world test"}]}`)
	result := q.QuotaCheck("key-C", "/claude", quota, body, makeProvider("messages"))

	if result.Allowed {
		t.Error("expected denied: shared pool is full")
	}
}

// TestCheck_Shared_Daily_AllowedWhenPoolNotFull verifies shared quota allows requests when pool has remaining capacity.
func TestCheck_Shared_Daily_AllowedWhenPoolNotFull(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedDaily(t, q, "key-A", "/claude", today, 3000)
	seedDaily(t, q, "key-B", "/claude", today, 3000)

	quota := makeQuota("shared", 10000, 0)
	result := q.QuotaCheck("key-C", "/claude", quota, nil, makeProvider(""))
	if !result.Allowed {
		t.Errorf("shared pool has room (6000/10000), should allow: %s", result.DenyReason)
	}
}

// TestCheck_Shared_DoesNotMixRoutes verifies shared quota does not cross-contaminate different routes.
func TestCheck_Shared_DoesNotMixRoutes(t *testing.T) {
	q := newQuotaTestDB(t)
	today := time.Now().Format("2006-01-02")

	seedDaily(t, q, "key-A", "/gemini", today, 9999)

	quota := makeQuota("shared", 10000, 0)
	result := q.QuotaCheck("key-A", "/claude", quota, nil, makeProvider(""))
	if !result.Allowed {
		t.Errorf("/claude should be unaffected by /gemini usage, got denied: %s", result.DenyReason)
	}
}

// TestEstimateInputTokens_NoInputMessage verifies zero estimation when InputMessage is empty.
func TestEstimateInputTokens_NoInputMessage(t *testing.T) {
	provider := makeProvider("")
	body := []byte(`{"messages":[{"role":"user","content":"hello world"}]}`)
	got := estimateInputTokens(body, provider)
	if got != 0 {
		t.Errorf("expected 0 when InputMessage is empty, got %d", got)
	}
}

// TestEstimateInputTokens_NilProvider verifies zero estimation when provider is nil.
func TestEstimateInputTokens_NilProvider(t *testing.T) {
	got := estimateInputTokens([]byte(`{"messages":[]}`), nil)
	if got != 0 {
		t.Errorf("expected 0 for nil provider, got %d", got)
	}
}

// TestEstimateInputTokens_EmptyBody verifies zero estimation when body is empty.
func TestEstimateInputTokens_EmptyBody(t *testing.T) {
	provider := makeProvider("messages")
	got := estimateInputTokens(nil, provider)
	if got != 0 {
		t.Errorf("expected 0 for empty body, got %d", got)
	}
}

// TestEstimateInputTokens_FieldNotFound verifies zero estimation when InputMessage path is missing.
func TestEstimateInputTokens_FieldNotFound(t *testing.T) {
	provider := makeProvider("messages")
	body := []byte(`{"model":"gpt-4","stream":true}`)
	got := estimateInputTokens(body, provider)
	if got != 0 {
		t.Errorf("expected 0 when field not found, got %d", got)
	}
}

// TestEstimateInputTokens_ReturnsNonZeroForValidField verifies positive estimation for valid payload.
func TestEstimateInputTokens_ReturnsNonZeroForValidField(t *testing.T) {
	provider := makeProvider("messages")
	body := []byte(`{"messages":[{"role":"user","content":"hello world this is a test message with enough text"}]}`)
	got := estimateInputTokens(body, provider)
	if got <= 0 {
		t.Errorf("expected positive estimate, got %d", got)
	}
}

// TestGetDailyStats_AggregatesByDateRouteAndKey verifies daily stats are grouped by date, route, and local key.
func TestGetDailyStats_AggregatesByDateRouteAndKey(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	s := NewUsageStore(db)
	seedTestData(t, b)

	rows, err := s.GetDailyStats("2026-06-20", "2026-06-21")
	if err != nil {
		t.Fatalf("GetDailyStats() error: %v", err)
	}

	// Expect 3 groups:
	// 2026-06-21 /v1/embeddings sk-local-01
	// 2026-06-20 /v1/chat       sk-local-01
	// 2026-06-20 /v1/chat       sk-local-02
	if len(rows) != 3 {
		t.Fatalf("len(rows) = %d, want 3", len(rows))
	}

	// Row 0: latest date first.
	if rows[0].Date != "2026-06-21" ||
		rows[0].RoutePath != "/v1/embeddings" ||
		rows[0].LocalKey != "sk-local-01" ||
		rows[0].TotalRequests != 1 ||
		rows[0].PromptTokens != 5 ||
		rows[0].CompletionTokens != 0 ||
		rows[0].TotalTokens != 5 {
		t.Fatalf("unexpected row[0]: %+v", rows[0])
	}
	if rows[0].LastActivity == "" {
		t.Fatalf("row[0].LastActivity should not be empty")
	}

	// Row 1/2 are the two keys on 2026-06-20, order between them may vary by DB tie-break.
	validateChatRow := func(r DailyStatRow) bool {
		return r.Date == "2026-06-20" &&
			r.RoutePath == "/v1/chat" &&
			(r.LocalKey == "sk-local-01" || r.LocalKey == "sk-local-02") &&
			r.TotalRequests == 1 &&
			r.LastActivity != ""
	}

	if !validateChatRow(rows[1]) || !validateChatRow(rows[2]) {
		t.Fatalf("unexpected chat rows: row[1]=%+v row[2]=%+v", rows[1], rows[2])
	}

	// Verify per-key token values precisely.
	for _, r := range rows[1:] {
		switch r.LocalKey {
		case "sk-local-01":
			if r.PromptTokens != 10 || r.CompletionTokens != 100 || r.TotalTokens != 110 {
				t.Fatalf("unexpected sk-local-01 daily totals: %+v", r)
			}
		case "sk-local-02":
			if r.PromptTokens != 20 || r.CompletionTokens != 200 || r.TotalTokens != 220 {
				t.Fatalf("unexpected sk-local-02 daily totals: %+v", r)
			}
		default:
			t.Fatalf("unexpected local key in daily rows: %+v", r)
		}
	}
}

// TestGetMonthlyStats_AggregatesByMonthRouteAndKey verifies monthly stats are grouped by month, route, and local key.
func TestGetMonthlyStats_AggregatesByMonthRouteAndKey(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	s := NewUsageStore(db)
	seedTestData(t, b)

	rows, err := s.GetMonthlyStats(2026)
	if err != nil {
		t.Fatalf("GetMonthlyStats() error: %v", err)
	}

	// Expect 3 groups:
	// 2026-06 /v1/chat       sk-local-01  => 110
	// 2026-06 /v1/chat       sk-local-02  => 220
	// 2026-06 /v1/embeddings sk-local-01  => 5
	// 2026-05 /v1/chat       sk-local-01  => 550
	// Actually total should be 4 groups.
	if len(rows) != 4 {
		t.Fatalf("len(rows) = %d, want 4", len(rows))
	}

	// Build index for stable assertions.
	type key struct {
		month string
		route string
		local string
	}
	got := map[key]MonthlyStatRow{}
	for _, r := range rows {
		got[key{month: r.Month, route: r.RoutePath, local: r.LocalKey}] = r
	}

	assertMonthly := func(month, route, local string, req, p, c, total int64) {
		k := key{month: month, route: route, local: local}
		r, ok := got[k]
		if !ok {
			t.Fatalf("missing monthly row: %+v", k)
		}
		if r.TotalRequests != req || r.PromptTokens != p || r.CompletionTokens != c || r.TotalTokens != total {
			t.Fatalf("unexpected monthly row for %+v: %+v", k, r)
		}
	}

	assertMonthly("2026-06", "/v1/chat", "sk-local-01", 1, 10, 100, 110)
	assertMonthly("2026-06", "/v1/chat", "sk-local-02", 1, 20, 200, 220)
	assertMonthly("2026-06", "/v1/embeddings", "sk-local-01", 1, 5, 0, 5)
	assertMonthly("2026-05", "/v1/chat", "sk-local-01", 1, 50, 500, 550)
}

// TestGetVerboseLogs_LimitAndOrder verifies verbose logs return id, descending order, and limit behavior.
func TestGetVerboseLogs_LimitAndOrder(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	s := NewUsageStore(db)
	seedTestData(t, b)

	rows, err := s.GetVerboseLogs(2)
	if err != nil {
		t.Fatalf("GetVerboseLogs() error: %v", err)
	}

	if len(rows) != 2 {
		t.Fatalf("len(rows) = %d, want 2", len(rows))
	}

	// Newest first:
	// 2026-06-21 09:15:00 => /v1/embeddings, total=5
	// 2026-06-20 15:30:00 => /v1/chat, total=220
	if rows[0].ID == 0 || rows[1].ID == 0 {
		t.Fatalf("ID should not be empty: row0=%+v row1=%+v", rows[0], rows[1])
	}
	if rows[0].RoutePath != "/v1/embeddings" || rows[0].LocalKey != "sk-local-01" || rows[0].TotalTokens != 5 {
		t.Fatalf("unexpected first row: %+v", rows[0])
	}
	if rows[1].RoutePath != "/v1/chat" || rows[1].LocalKey != "sk-local-02" || rows[1].TotalTokens != 220 {
		t.Fatalf("unexpected second row: %+v", rows[1])
	}
	if rows[0].Timestamp == "" || rows[1].Timestamp == "" {
		t.Fatalf("timestamp should not be empty: row0=%+v row1=%+v", rows[0], rows[1])
	}
}
