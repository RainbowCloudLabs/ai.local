-- Table A: Cached Daily Summary
CREATE TABLE IF NOT EXISTS daily_usages (
    local_key TEXT NOT NULL,
    route_path TEXT NOT NULL,
    usage_date TEXT NOT NULL,               -- YYYY-MM-DD
    total_requests INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    daily_tokens INTEGER NOT NULL DEFAULT 0, -- total_tokens
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (local_key, route_path, usage_date)
);

CREATE INDEX IF NOT EXISTS idx_daily_route ON daily_usages(route_path, usage_date);

-- Table C: Cached Monthly Summary
CREATE TABLE IF NOT EXISTS monthly_usages (
    local_key TEXT NOT NULL,
    route_path TEXT NOT NULL,
    usage_month TEXT NOT NULL,               -- YYYY-MM
    total_requests INTEGER NOT NULL DEFAULT 0,
    prompt_tokens INTEGER NOT NULL DEFAULT 0,
    completion_tokens INTEGER NOT NULL DEFAULT 0,
    monthly_tokens INTEGER NOT NULL DEFAULT 0, -- total_tokens
    updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (local_key, route_path, usage_month)
);

CREATE INDEX IF NOT EXISTS idx_monthly_route ON monthly_usages(route_path, usage_month);

-- Table B: Immutable Audit Trail Logs
CREATE TABLE IF NOT EXISTS usage_logs (
    id INTEGER PRIMARY KEY AUTOINCREMENT,
    local_key TEXT NOT NULL,
    route_path TEXT NOT NULL,
    client_ip TEXT NOT NULL,
    prompt_tokens INTEGER NOT NULL,
    completion_tokens INTEGER NOT NULL,
    total_tokens INTEGER NOT NULL,
    created_at DATETIME DEFAULT CURRENT_TIMESTAMP
);
