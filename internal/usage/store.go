package usage

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/RainbowCloudLabs/ai.local/internal/apml"
	"github.com/RainbowCloudLabs/ai.local/internal/logx"
	"github.com/tidwall/gjson"
)

// UsageStore performs pre-flight token budget validation before forwarding
// a request to the upstream AI provider.
//
// It supports two quota modes defined in APML:
//   - per_key: each internal key has its own independent daily/monthly budget
//   - shared: all keys on the same route share a single pool
type UsageStore struct {
	db *sql.DB
}

// NewUsageStore creates a UsageStore bound to the given SQLite handle.
func NewUsageStore(db *sql.DB) *UsageStore {
	return &UsageStore{db: db}
}

// CheckResult represents the decision payload returned by QuotaCheck.
type CheckResult struct {
	Allowed       bool
	DenyReason    string
	CurrentTokens int64 // current usage before this request
	QuotaLimit    int64 // the limit being enforced
	Estimated     int64 // estimated tokens for this request
}

// DailyStatRow represents one aggregated daily usage row.
type DailyStatRow struct {
	Date             string
	RoutePath        string
	LocalKey         string
	TotalRequests    int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
	LastActivity     string
}

// MonthlyStatRow represents one aggregated monthly usage row.
type MonthlyStatRow struct {
	Month            string
	RoutePath        string
	LocalKey         string
	TotalRequests    int64
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// VerboseStatRow represents one raw usage log row for verbose stats output.
type VerboseStatRow struct {
	ID               int64
	Timestamp        string
	LocalKey         string
	RoutePath        string
	ClientIP         string
	PromptTokens     int64
	CompletionTokens int64
	TotalTokens      int64
}

// QuotaCheck validates whether a request should be allowed based on quota.
//
// Parameters:
//   - localKey: the internal key (sk-local-xxx) from the request header
//   - routePath: the route path (for example, "/claude")
//   - quota: the QuotaDetail resolved from APML for this route
//   - requestBody: raw request body bytes used to estimate input tokens
//   - provider: APML provider config used for InputMessage field lookup
func (u *UsageStore) QuotaCheck(
	localKey string,
	routePath string,
	quota apml.QuotaDetail,
	requestBody []byte,
	provider *apml.ProviderConfig,
) CheckResult {
	// unlimited quota (daily=0 and monthly=0 means no limit)
	if quota.Daily == 0 && quota.Monthly == 0 {
		logx.AppDebugf("quota check bypass: unlimited allocation policy enforced on route %s", routePath)
		return CheckResult{Allowed: true}
	}

	estimated := estimateInputTokens(requestBody, provider)
	now := time.Now()
	today := now.Format("2006-01-02")
	thisMonth := now.Format("2006-01")

	logx.AppDebugf("quota pre-flight assessment: route=%s, key=%s, mode=%s, estimated_input=%d", routePath, localKey, quota.Mode, estimated)

	// daily check
	if quota.Daily > 0 {
		current, err := u.queryTokens(quota.Mode, localKey, routePath, "daily", today)
		if err != nil {
			logx.AppDebugf("quota db failure (fail-open engaged): daily lookup failed for route %s: %v", routePath, err)
			return CheckResult{Allowed: true}
		}
		if current+estimated > quota.Daily {
			logx.AppDebugf("quota violation: daily limit breach on route %s (used=%d, estimated=%d, limit=%d)", routePath, current, estimated, quota.Daily)
			return CheckResult{
				Allowed:       false,
				DenyReason:    fmt.Sprintf("daily quota exceeded: used %d + estimated %d > limit %d", current, estimated, quota.Daily),
				CurrentTokens: current,
				QuotaLimit:    quota.Daily,
				Estimated:     estimated,
			}
		}
	}

	// monthly check
	if quota.Monthly > 0 {
		current, err := u.queryTokens(quota.Mode, localKey, routePath, "monthly", thisMonth)
		if err != nil {
			logx.AppDebugf("quota db failure (fail-open engaged): monthly lookup failed for route %s: %v", routePath, err)
			return CheckResult{Allowed: true}
		}
		if current+estimated > quota.Monthly {
			logx.AppDebugf("quota violation: monthly limit breach on route %s (used=%d, estimated=%d, limit=%d)", routePath, current, estimated, quota.Monthly)
			return CheckResult{
				Allowed:       false,
				DenyReason:    fmt.Sprintf("monthly quota exceeded: used %d + estimated %d > limit %d", current, estimated, quota.Monthly),
				CurrentTokens: current,
				QuotaLimit:    quota.Monthly,
				Estimated:     estimated,
			}
		}
	}

	logx.AppDebugf("quota verification cleared for route %s", routePath)
	return CheckResult{Allowed: true, Estimated: estimated}
}

// queryTokens fetches current token usage from SQLite.
//
// mode "per_key": looks up by (local_key, route_path, period)
// mode "shared": sums all keys on (route_path, period)
// period "daily": uses daily_usages, period value format "YYYY-MM-DD"
// period "monthly": uses monthly_usages, period value format "YYYY-MM"
func (u *UsageStore) queryTokens(mode, localKey, routePath, period, periodValue string) (int64, error) {
	var (
		query string
		args  []interface{}
	)

	switch period {
	case "daily":
		if mode == "shared" {
			query = `SELECT COALESCE(SUM(daily_tokens), 0) FROM daily_usages
					 WHERE route_path = ? AND usage_date = ?`
			args = []interface{}{routePath, periodValue}
		} else {
			query = `SELECT COALESCE(daily_tokens, 0) FROM daily_usages
					 WHERE local_key = ? AND route_path = ? AND usage_date = ?`
			args = []interface{}{localKey, routePath, periodValue}
		}
	case "monthly":
		if mode == "shared" {
			query = `SELECT COALESCE(SUM(monthly_tokens), 0) FROM monthly_usages
					 WHERE route_path = ? AND usage_month = ?`
			args = []interface{}{routePath, periodValue}
		} else {
			query = `SELECT COALESCE(monthly_tokens, 0) FROM monthly_usages
					 WHERE local_key = ? AND route_path = ? AND usage_month = ?`
			args = []interface{}{localKey, routePath, periodValue}
		}
	default:
		return 0, fmt.Errorf("unknown period: %s", period)
	}

	var tokens int64
	err := u.db.QueryRow(query, args...).Scan(&tokens)
	if err == sql.ErrNoRows {
		return 0, nil
	}
	return tokens, err
}

// estimateInputTokens provides a rough token estimate from request payload.
//
// It extracts the field specified by provider.InputMessage from the request body
// and applies an approximation:
//
//	tokens ≈ (rawLen * 6) / 10
//
// This is intentionally approximate for pre-flight quota protection.
func estimateInputTokens(body []byte, provider *apml.ProviderConfig) int64 {
	if provider == nil || provider.InputMessage == "" || len(body) == 0 {
		return 0
	}

	result := gjson.GetBytes(body, provider.InputMessage)
	if !result.Exists() {
		return 0
	}

	rawLen := int64(len(result.Raw))
	return (rawLen * 6) / 10
}

// GetDailyStats aggregates cached daily usage rows within [startDate, endDate].
func (s *UsageStore) GetDailyStats(startDate, endDate string) ([]DailyStatRow, error) {
	query := `
		SELECT
			usage_date,
			route_path,
			local_key,
			COALESCE(total_requests, 0) AS total_requests,
			COALESCE(prompt_tokens, 0) AS total_prompt,
			COALESCE(completion_tokens, 0) AS total_completion,
			COALESCE(daily_tokens, 0) AS total_combined,
			COALESCE(CAST(updated_at AS TEXT), '') AS last_activity
		FROM daily_usages
		WHERE usage_date BETWEEN ? AND ?
		ORDER BY usage_date DESC, total_combined DESC;
	`

	rows, err := s.db.Query(query, startDate, endDate)
	if err != nil {
		return nil, fmt.Errorf("execute daily aggregate query: %w", err)
	}
	defer rows.Close()

	var result []DailyStatRow
	for rows.Next() {
		var r DailyStatRow
		if err := rows.Scan(
			&r.Date,
			&r.RoutePath,
			&r.LocalKey,
			&r.TotalRequests,
			&r.PromptTokens,
			&r.CompletionTokens,
			&r.TotalTokens,
			&r.LastActivity,
		); err != nil {
			return nil, fmt.Errorf("scan daily stat row: %w", err)
		}
		result = append(result, r)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate daily stat rows: %w", err)
	}

	return result, nil
}

// GetMonthlyStats returns cached monthly usage rows for the specified year.
func (s *UsageStore) GetMonthlyStats(year int) ([]MonthlyStatRow, error) {
	yearStr := fmt.Sprintf("%04d", year)

	query := `
		SELECT
			usage_month,
			route_path,
			local_key,
			COALESCE(total_requests, 0) AS total_requests,
			COALESCE(prompt_tokens, 0) AS total_prompt,
			COALESCE(completion_tokens, 0) AS total_completion,
			COALESCE(monthly_tokens, 0) AS total_combined
		FROM monthly_usages
		WHERE substr(usage_month, 1, 4) = ?
		ORDER BY usage_month DESC, total_combined DESC;
	`

	rows, err := s.db.Query(query, yearStr)
	if err != nil {
		return nil, fmt.Errorf("execute monthly aggregate query: %w", err)
	}
	defer rows.Close()

	var result []MonthlyStatRow
	for rows.Next() {
		var r MonthlyStatRow
		if err := rows.Scan(
			&r.Month,
			&r.RoutePath,
			&r.LocalKey,
			&r.TotalRequests,
			&r.PromptTokens,
			&r.CompletionTokens,
			&r.TotalTokens,
		); err != nil {
			return nil, fmt.Errorf("scan monthly stat row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate monthly stat rows: %w", err)
	}

	return result, nil
}

// GetVerboseLogs returns the latest usage logs in descending creation time order.
func (s *UsageStore) GetVerboseLogs(limit int) ([]VerboseStatRow, error) {
	query := `
		SELECT
			id,
			route_path,
			local_key,
			client_ip,
			prompt_tokens,
			completion_tokens,
			total_tokens,
			CAST(created_at AS TEXT) AS created_at
		FROM usage_logs
		ORDER BY id DESC
		LIMIT ?;
`
	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, fmt.Errorf("execute verbose log stream query: %w", err)
	}
	defer rows.Close()

	var result []VerboseStatRow
	for rows.Next() {
		var r VerboseStatRow
		if err := rows.Scan(
			&r.ID,
			&r.RoutePath,
			&r.LocalKey,
			&r.ClientIP,
			&r.PromptTokens,
			&r.CompletionTokens,
			&r.TotalTokens,
			&r.Timestamp,
		); err != nil {
			return nil, fmt.Errorf("scan verbose log row: %w", err)
		}
		result = append(result, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate verbose log rows: %w", err)
	}

	return result, nil
}
