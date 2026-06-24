package usage

import (
	"database/sql"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

// newTestDB opens an in-memory SQLite database with the usage schema applied.
// Each call returns a fresh, isolated database -- no file ever touches disk.
func newTestDB(t *testing.T) *sql.DB {
	t.Helper()

	db, err := sql.Open("sqlite", ":memory:")
	if err != nil {
		t.Fatalf("failed to open in-memory sqlite: %v", err)
	}
	t.Cleanup(func() { db.Close() })

	if err := InitSchema(db); err != nil {
		t.Fatalf("InitSchema() error: %v", err)
	}
	return db
}

// queryInt is a small helper to run a single-row, single-column integer query.
func queryInt(t *testing.T, db *sql.DB, query string, args ...interface{}) int64 {
	t.Helper()
	var n int64
	if err := db.QueryRow(query, args...).Scan(&n); err != nil {
		t.Fatalf("query %q error: %v", query, err)
	}
	return n
}

// ---- InitSchema ----

func TestInitSchema_CreatesAllTables(t *testing.T) {
	db := newTestDB(t)

	tables := []string{"usage_logs", "daily_usages", "monthly_usages"}
	for _, table := range tables {
		var name string
		err := db.QueryRow(
			"SELECT name FROM sqlite_master WHERE type='table' AND name=?", table,
		).Scan(&name)
		if err != nil {
			t.Errorf("table %q not found: %v", table, err)
		}
	}
}

func TestInitSchema_IsIdempotent(t *testing.T) {
	db := newTestDB(t)

	// Calling InitSchema a second time should not error (CREATE TABLE IF NOT EXISTS).
	if err := InitSchema(db); err != nil {
		t.Errorf("second InitSchema() call returned error: %v", err)
	}
}

// ---- executeTripleWrite ----

func TestExecuteTripleWrite_InsertsLogRow(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)

	rec := &Record{
		LocalKey:         "sk-local-abc",
		RoutePath:        "/claude",
		ClientIP:         "127.0.0.1",
		PromptTokens:     100,
		CompletionTokens: 50,
		TotalTokens:      150,
		CreatedAt:        time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
	}

	if err := b.executeTripleWrite(rec); err != nil {
		t.Fatalf("executeTripleWrite() error: %v", err)
	}

	count := queryInt(t, db, "SELECT COUNT(*) FROM usage_logs WHERE local_key = ?", rec.LocalKey)
	if count != 1 {
		t.Errorf("usage_logs row count = %d, want 1", count)
	}

	var promptTokens, completionTokens, totalTokens int64
	err := db.QueryRow(
		"SELECT prompt_tokens, completion_tokens, total_tokens FROM usage_logs WHERE local_key = ?",
		rec.LocalKey,
	).Scan(&promptTokens, &completionTokens, &totalTokens)
	if err != nil {
		t.Fatalf("query usage_logs error: %v", err)
	}
	if promptTokens != 100 || completionTokens != 50 || totalTokens != 150 {
		t.Errorf("usage_logs tokens = (%d, %d, %d), want (100, 50, 150)",
			promptTokens, completionTokens, totalTokens)
	}
}

func TestExecuteTripleWrite_CreatesDailyAndMonthlyRows(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)

	rec := &Record{
		LocalKey:    "sk-local-abc",
		RoutePath:   "/claude",
		TotalTokens: 150,
		CreatedAt:   time.Date(2026, 6, 21, 10, 0, 0, 0, time.UTC),
	}

	if err := b.executeTripleWrite(rec); err != nil {
		t.Fatalf("executeTripleWrite() error: %v", err)
	}

	daily := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND usage_date = ?",
		rec.LocalKey, "2026-06-21",
	)
	if daily != 150 {
		t.Errorf("daily_tokens = %d, want 150", daily)
	}

	monthly := queryInt(t, db,
		"SELECT monthly_tokens FROM monthly_usages WHERE local_key = ? AND usage_month = ?",
		rec.LocalKey, "2026-06",
	)
	if monthly != 150 {
		t.Errorf("monthly_tokens = %d, want 150", monthly)
	}
}

func TestExecuteTripleWrite_AccumulatesOnSameDay(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)

	day := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	rec1 := &Record{LocalKey: "sk-local-abc", RoutePath: "/claude", TotalTokens: 100, CreatedAt: day}
	rec2 := &Record{LocalKey: "sk-local-abc", RoutePath: "/claude", TotalTokens: 200, CreatedAt: day.Add(time.Hour)}

	if err := b.executeTripleWrite(rec1); err != nil {
		t.Fatalf("first executeTripleWrite() error: %v", err)
	}
	if err := b.executeTripleWrite(rec2); err != nil {
		t.Fatalf("second executeTripleWrite() error: %v", err)
	}

	daily := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND usage_date = ?",
		"sk-local-abc", "2026-06-21",
	)
	if daily != 300 {
		t.Errorf("daily_tokens = %d, want 300 (100+200 accumulated)", daily)
	}

	logCount := queryInt(t, db, "SELECT COUNT(*) FROM usage_logs WHERE local_key = ?", "sk-local-abc")
	if logCount != 2 {
		t.Errorf("usage_logs row count = %d, want 2 (one per request, not merged)", logCount)
	}
}

func TestExecuteTripleWrite_SeparatesDifferentKeysAndRoutes(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)

	day := time.Date(2026, 6, 21, 9, 0, 0, 0, time.UTC)
	recA := &Record{LocalKey: "key-A", RoutePath: "/claude", TotalTokens: 100, CreatedAt: day}
	recB := &Record{LocalKey: "key-B", RoutePath: "/claude", TotalTokens: 999, CreatedAt: day}
	recC := &Record{LocalKey: "key-A", RoutePath: "/gemini", TotalTokens: 50, CreatedAt: day}

	for _, r := range []*Record{recA, recB, recC} {
		if err := b.executeTripleWrite(r); err != nil {
			t.Fatalf("executeTripleWrite() error: %v", err)
		}
	}

	dailyA := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND route_path = ? AND usage_date = ?",
		"key-A", "/claude", "2026-06-21",
	)
	if dailyA != 100 {
		t.Errorf("key-A /claude daily_tokens = %d, want 100 (must not merge with key-B)", dailyA)
	}

	dailyAGemini := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND route_path = ? AND usage_date = ?",
		"key-A", "/gemini", "2026-06-21",
	)
	if dailyAGemini != 50 {
		t.Errorf("key-A /gemini daily_tokens = %d, want 50 (must not merge with /claude)", dailyAGemini)
	}
}

func TestExecuteTripleWrite_DefaultsCreatedAtWhenZero(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)

	rec := &Record{
		LocalKey:    "sk-local-abc",
		RoutePath:   "/claude",
		TotalTokens: 10,
		// CreatedAt intentionally left zero-value
	}

	if err := b.executeTripleWrite(rec); err != nil {
		t.Fatalf("executeTripleWrite() error: %v", err)
	}

	today := time.Now().Format("2006-01-02")
	daily := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND usage_date = ?",
		"sk-local-abc", today,
	)
	if daily != 10 {
		t.Errorf("daily_tokens = %d, want 10 (zero CreatedAt should default to now)", daily)
	}
}

// ---- EmitNonBlocking / StartWorker / Stop (integration through the channel) ----

func TestUsageBackend_EmitAndProcessRecord(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	b.StartWorker()
	defer b.Stop()

	rec := &Record{
		LocalKey:    "sk-local-async",
		RoutePath:   "/claude",
		TotalTokens: 42,
		CreatedAt:   time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC),
	}
	b.EmitNonBlocking(rec)

	// Stop() drains the queue before returning, so after Stop the write
	// is guaranteed to have completed -- no need for a sleep/poll here.
	b.Stop()

	daily := queryInt(t, db,
		"SELECT daily_tokens FROM daily_usages WHERE local_key = ? AND usage_date = ?",
		"sk-local-async", "2026-06-21",
	)
	if daily != 42 {
		t.Errorf("daily_tokens = %d, want 42", daily)
	}
}

func TestUsageBackend_StopDrainsQueueBeforeExit(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	b.StartWorker()

	// Emit several records without waiting in between, then immediately Stop.
	// Stop() must drain all of them, not just whatever the worker had already
	// picked up off the channel.
	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < 20; i++ {
		b.EmitNonBlocking(&Record{
			LocalKey:    "sk-local-drain",
			RoutePath:   "/claude",
			TotalTokens: 1,
			CreatedAt:   day,
		})
	}
	b.Stop()

	logCount := queryInt(t, db, "SELECT COUNT(*) FROM usage_logs WHERE local_key = ?", "sk-local-drain")
	if logCount != 20 {
		t.Errorf("usage_logs row count = %d, want 20 (all records must be drained on Stop)", logCount)
	}
}

func TestUsageBackend_StopIsSafeToCallTwice(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	b.StartWorker()

	b.Stop()
	b.Stop() // must not panic or block forever (sync.Once protects close(done))
}

func TestUsageBackend_EmitNonBlocking_DropsWhenQueueFull(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	// Note: worker is intentionally NOT started, so the queue fills up
	// and EmitNonBlocking must drop excess records instead of blocking.

	day := time.Date(2026, 6, 21, 0, 0, 0, 0, time.UTC)
	for i := 0; i < defaultQueueSize+10; i++ {
		b.EmitNonBlocking(&Record{
			LocalKey:    "sk-local-overflow",
			RoutePath:   "/claude",
			TotalTokens: 1,
			CreatedAt:   day,
		})
	}

	if b.DroppedCount() != 10 {
		t.Errorf("DroppedCount() = %d, want 10 (defaultQueueSize+10 emitted, queue holds defaultQueueSize)",
			b.DroppedCount())
	}
}

func TestUsageBackend_DroppedCount_ZeroWhenNoOverflow(t *testing.T) {
	db := newTestDB(t)
	b := NewUsageBackend(db)
	b.StartWorker()
	defer b.Stop()

	b.EmitNonBlocking(&Record{LocalKey: "sk-local-x", RoutePath: "/claude", TotalTokens: 1})

	if b.DroppedCount() != 0 {
		t.Errorf("DroppedCount() = %d, want 0", b.DroppedCount())
	}
}
