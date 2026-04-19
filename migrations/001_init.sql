-- migrations/001_init.sql

CREATE TABLE IF NOT EXISTS funds (
    scheme_code     TEXT PRIMARY KEY,
    scheme_name     TEXT NOT NULL,
    amc             TEXT NOT NULL,
    category        TEXT NOT NULL,
    discovered_at   TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

CREATE TABLE IF NOT EXISTS nav_data (
    scheme_code     TEXT NOT NULL,
    nav_date        DATE NOT NULL,
    nav_value       NUMERIC(12, 4) NOT NULL,
    PRIMARY KEY (scheme_code, nav_date),
    FOREIGN KEY (scheme_code) REFERENCES funds(scheme_code)
);

-- Index for time-range queries (analytics engine will use this heavily)
CREATE INDEX IF NOT EXISTS idx_nav_data_scheme_date 
    ON nav_data (scheme_code, nav_date DESC);

CREATE TABLE IF NOT EXISTS analytics (
    scheme_code         TEXT NOT NULL,
    window_period       TEXT NOT NULL,   -- '1Y','3Y','5Y','10Y'  ← renamed
    rolling_min         NUMERIC(10, 4),
    rolling_max         NUMERIC(10, 4),
    rolling_median      NUMERIC(10, 4),
    rolling_p25         NUMERIC(10, 4),
    rolling_p75         NUMERIC(10, 4),
    max_drawdown        NUMERIC(10, 4),
    cagr_min            NUMERIC(10, 4),
    cagr_max            NUMERIC(10, 4),
    cagr_median         NUMERIC(10, 4),
    periods_analyzed    INT,
    data_start          DATE,
    data_end            DATE,
    total_days          INT,
    nav_data_points     INT,
    computed_at         TIMESTAMPTZ NOT NULL DEFAULT NOW(),
    PRIMARY KEY (scheme_code, window_period),
    FOREIGN KEY (scheme_code) REFERENCES funds(scheme_code)
);

CREATE TABLE IF NOT EXISTS sync_jobs (
    id              SERIAL PRIMARY KEY,
    job_type        TEXT NOT NULL,      -- 'backfill' | 'incremental'
    scheme_code     TEXT,               -- NULL means all schemes
    status          TEXT NOT NULL,      -- 'pending'|'running'|'done'|'failed'
    started_at      TIMESTAMPTZ,
    completed_at    TIMESTAMPTZ,
    error_msg       TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Rate limiter state: we persist request timestamps so restarts
-- don't reset our quota consumption
CREATE TABLE IF NOT EXISTS rate_limiter_log (
    id              BIGSERIAL PRIMARY KEY,
    requested_at    TIMESTAMPTZ NOT NULL DEFAULT NOW()
);

-- Only keep last 1 hour of logs (older rows are outside all windows)
CREATE INDEX IF NOT EXISTS idx_rate_limiter_log_time 
    ON rate_limiter_log (requested_at DESC);