package usage

import (
	"database/sql"
	_ "embed"
	"fmt"
	"sync"
	"time"

	"github.com/RainbowCloudLabs/ai.local/internal/logx"
	"github.com/RainbowCloudLabs/ai.local/schema"
)

// InitSchema ensures all required tables are securely bootstrapped in SQLite.
func InitSchema(db *sql.DB) error {
	// Enable WAL mode for high-performance reader/writer isolation
	if _, err := db.Exec("PRAGMA journal_mode=WAL;"); err != nil {
		return err
	}

	// Execute the embedded SQL migration script
	if _, err := db.Exec(schema.UsageSQL); err != nil {
		return err
	}
	return nil
}

// defaultQueueSize is the channel buffer depth. Sized to absorb short bursts
// of concurrent requests without blocking the proxy hot path; if the worker
// falls behind beyond this, EmitNonBlocking starts dropping records (logged).
const defaultQueueSize = 1024

// UsageBackend manages the buffered channel pipeline and the background
// database worker that persists token usage records to SQLite.
//
// Design notes:
//   - EmitNonBlocking is called from the proxy's hot path (ModifyResponse)
//     and must never block, even if the worker is backed up. A full channel
//     means we drop the record rather than stall a live client response.
//   - Exactly one worker goroutine drains the channel and writes to SQLite.
//     A single writer avoids SQLite write-lock contention and keeps the
//     daily/monthly UPSERT logic free of race conditions.
//   - Stop() drains any remaining buffered records before returning, so a
//     graceful shutdown (e.g. SIGTERM) does not silently lose usage data.
type UsageBackend struct {
	db    *sql.DB
	queue chan *Record

	wg       sync.WaitGroup
	stopOnce sync.Once
	done     chan struct{}

	mu      sync.Mutex
	dropped int64 // count of records dropped due to a full queue, for diagnostics
}

// NewUsageBackend constructs a backend bound to the given SQLite handle.
// Call StartWorker() to begin processing, and Stop() to drain and shut down.
func NewUsageBackend(db *sql.DB) *UsageBackend {
	return &UsageBackend{
		db:    db,
		queue: make(chan *Record, defaultQueueSize),
		done:  make(chan struct{}),
	}
}

// EmitNonBlocking pushes the record into the channel reservoir without
// blocking the proxy thread. If the queue is full, the record is dropped
// and a warning is logged -- this trades perfect accounting for guaranteed
// proxy responsiveness under load.
func (b *UsageBackend) EmitNonBlocking(rec *Record) {
	select {
	case b.queue <- rec:
		// queued successfully
	default:
		b.mu.Lock()
		b.dropped++
		count := b.dropped
		b.mu.Unlock()
		logx.AppWarnf(
			"queue full, dropping usage record (local_key=%s route=%s) — %d dropped total",
			rec.LocalKey, rec.RoutePath, count,
		)
	}
}

// DroppedCount returns how many records have been dropped due to queue
// overflow since startup. Intended for health/metrics reporting.
func (b *UsageBackend) DroppedCount() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.dropped
}

// StartWorker boots the single-threaded SQL transaction execution loop.
// Safe to call once; call Stop() to terminate gracefully.
func (b *UsageBackend) StartWorker() {
	b.wg.Add(1)
	go func() {
		defer b.wg.Done()
		for {
			select {
			case rec := <-b.queue:
				b.writeRecord(rec)
			case <-b.done:
				// Drain whatever is left in the buffer before exiting,
				// so a graceful shutdown does not lose queued records.
				for {
					select {
					case rec := <-b.queue:
						b.writeRecord(rec)
					default:
						return
					}
				}
			}
		}
	}()
}

// Stop signals the worker to drain remaining records and exit, then blocks
// until it has fully stopped. Safe to call multiple times.
func (b *UsageBackend) Stop() {
	b.stopOnce.Do(func() {
		close(b.done)
	})
	b.wg.Wait()
}

// writeRecord executes the triple-write transaction and logs on failure.
// A failed write is never retried automatically -- retrying inline would
// block the single worker goroutine and risk unbounded queue growth under
// a persistent DB error. The error is logged so the gap is visible to an
// operator rather than silently lost.
func (b *UsageBackend) writeRecord(rec *Record) {
	if err := b.executeTripleWrite(rec); err != nil {
		logx.AppErrorf(
			"failed to persist usage record (local_key=%s route=%s tokens=%d): %v",
			rec.LocalKey, rec.RoutePath, rec.TotalTokens, err,
		)
	}
}

// executeTripleWrite atomically writes one usage record across all three
// tables defined in schema/usage.sql:
//
//  1. usage_logs      — immutable audit log, one row per request
//  2. daily_usages    — UPSERT, running total for (key, route, date)
//  3. monthly_usages  — UPSERT, running total for (key, route, month)
//
// All three writes share a single transaction so a crash or error mid-write
// cannot leave the audit log and the quota caches out of sync.
func (b *UsageBackend) executeTripleWrite(rec *Record) error {
	tx, err := b.db.Begin()
	if err != nil {
		return fmt.Errorf("begin transaction: %w", err)
	}
	defer tx.Rollback() // no-op if tx.Commit() succeeds

	createdAt := rec.CreatedAt
	if createdAt.IsZero() {
		createdAt = time.Now()
	}
	usageDate := createdAt.Format("2006-01-02")
	usageMonth := createdAt.Format("2006-01")

	// 1. Immutable audit log entry
	_, err = tx.Exec(
		`INSERT INTO usage_logs
			(local_key, route_path, client_ip, prompt_tokens, completion_tokens, total_tokens, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		rec.LocalKey, rec.RoutePath, rec.ClientIP,
		rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
		createdAt,
	)
	if err != nil {
		return fmt.Errorf("insert usage_logs: %w", err)
	}

	// 2. Daily cache UPSERT
	_, err = tx.Exec(
		`INSERT INTO daily_usages
		(local_key, route_path, usage_date, total_requests, prompt_tokens, completion_tokens, daily_tokens, updated_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	 ON CONFLICT(local_key, route_path, usage_date)
	 DO UPDATE SET
		total_requests    = daily_usages.total_requests + excluded.total_requests,
		prompt_tokens     = daily_usages.prompt_tokens + excluded.prompt_tokens,
		completion_tokens = daily_usages.completion_tokens + excluded.completion_tokens,
		daily_tokens      = daily_usages.daily_tokens + excluded.daily_tokens,
		updated_at        = CURRENT_TIMESTAMP`,
		rec.LocalKey, rec.RoutePath, usageDate,
		1, rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
	)
	if err != nil {
		return fmt.Errorf("upsert daily_usages: %w", err)
	}

	// 3. Monthly cache UPSERT
	_, err = tx.Exec(
		`INSERT INTO monthly_usages
		(local_key, route_path, usage_month, total_requests, prompt_tokens, completion_tokens, monthly_tokens, updated_at)
	 VALUES (?, ?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	 ON CONFLICT(local_key, route_path, usage_month)
	 DO UPDATE SET
		total_requests    = monthly_usages.total_requests + excluded.total_requests,
		prompt_tokens     = monthly_usages.prompt_tokens + excluded.prompt_tokens,
		completion_tokens = monthly_usages.completion_tokens + excluded.completion_tokens,
		monthly_tokens    = monthly_usages.monthly_tokens + excluded.monthly_tokens,
		updated_at        = CURRENT_TIMESTAMP`,
		rec.LocalKey, rec.RoutePath, usageMonth,
		1, rec.PromptTokens, rec.CompletionTokens, rec.TotalTokens,
	)
	if err != nil {
		return fmt.Errorf("upsert monthly_usages: %w", err)
	}

	if err := tx.Commit(); err != nil {
		return fmt.Errorf("commit transaction: %w", err)
	}
	return nil
}
