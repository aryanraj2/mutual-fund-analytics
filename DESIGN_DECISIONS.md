# DESIGN_DECISIONS.md

## Overview

This document explains the key architectural and implementation decisions made
while building the Mutual Fund Analytics service. Each section covers the
problem, the options considered, the choice made, and why.

---

## 1. Rate Limiting Strategy

### Problem
The mfapi.in API enforces three simultaneous constraints:
- 2 requests/second
- 50 requests/minute  
- 300 requests/hour

All three must be satisfied at the same time. A request that passes the
per-second check might still violate the per-minute or per-hour limit.

### Options Considered

| Algorithm | Pros | Cons |
|---|---|---|
| Fixed Window | Simple to implement | Double-burst at window boundaries |
| Token Bucket | Smooth, allows small bursts | Complex to persist state |
| Sliding Window | Accurate, no boundary burst | Slightly more memory |

### Decision: Sliding Window Counters

We maintain three independent in-memory slices of timestamps — one per window
(second, minute, hour). Before every outbound request:

1. Prune timestamps older than the window duration
2. Check count against the limit for each window
3. If ALL three pass → allow and record the timestamp
4. If ANY fails → calculate exact wait time and sleep

wait_time = oldest_timestamp_in_window + window_duration - now

This gives us the minimum possible wait rather than a fixed sleep, which
maximises throughput within the quota.

### Proof of Correctness

- Per-second: window slice never holds more than 2 entries within any 1-second
  span. Enforced by pruning before every check.
- Per-minute: same guarantee across 60 seconds.
- Per-hour: same guarantee across 3600 seconds.
- All three checks are inside a mutex — no race conditions under concurrent
  goroutines.

### State Persistence

Every allowed request is written to the `rate_limiter_log` table
asynchronously. On restart, the limiter reads back entries from the last hour
and reconstructs all three windows. This means quota consumption survives
process restarts — we never accidentally reset our hourly budget.

---

## 2. Backfill Orchestration Within Quota

### Problem
We need full NAV history (up to 10 years) for 10 schemes. With a 300/hour
limit this needs careful planning.

### Key Insight

mfapi.in returns the **complete NAV history in a single API call** per scheme.
So backfilling 10 schemes = 10 API calls + 1 discovery call = 11 calls total.
This fits comfortably within even the per-minute limit (50/min).

### Strategy
1 call  → GET /mf (fetch all ~37,000 scheme codes for discovery)
10 calls → GET /mf/{code} (one per scheme, full history included)
─────────────────────────────────────────────────────────────────
11 calls total for complete backfill

Every call goes through the rate limiter's `Wait()` method which blocks until
all three windows have capacity. This makes the pipeline automatically
compliant without any manual throttling logic.

### Resumability

Each scheme gets a row in `sync_jobs` with status `pending → running → done`
(or `failed`). If the process crashes mid-backfill:

1. On restart, `RunBackfill` checks which schemes already have NAV data
2. Schemes with >10 existing NAV points are skipped
3. Only failed or missing schemes are retried

`BulkUpsertNAV` uses `ON CONFLICT DO NOTHING` so re-inserting already-stored
NAV rows is safe and idempotent.

---

## 3. Storage Schema for Time-Series NAV Data

### Decision: PostgreSQL with a normalised schema

```sql
funds        → scheme metadata (one row per fund)
nav_data     → daily NAV values (one row per fund per day)
analytics    → pre-computed metrics (one row per fund per window)
sync_jobs    → pipeline state for resumability
rate_limiter_log → persisted request timestamps for quota survival
```

### Why PostgreSQL over alternatives

| Option | Reason rejected |
|---|---|
| TimescaleDB | Extra dependency, overkill for 10 funds |
| InfluxDB | No SQL joins, harder to serve ranking queries |
| SQLite | Not safe for concurrent API + pipeline writes |
| Redis | Volatile, no persistent time-series |

### NAV Table Design

```sql
PRIMARY KEY (scheme_code, nav_date)
INDEX ON (scheme_code, nav_date DESC)
```

The composite primary key enforces uniqueness and the descending index makes
"get latest NAV" a single index scan. For analytics queries that load full
history, the ascending scan order matches our computation order naturally.

### Why Not Store Pre-Aggregated Candles

Some time-series DBs store OHLC candles. We store raw daily NAV because:
- NAV is already a daily value (no intraday data)
- We need the full series for rolling window computation
- Raw data lets us answer arbitrary future queries (e.g. post-COVID analysis)

---

## 4. Pre-Computation vs On-Demand Trade-offs

### Decision: Pre-compute all analytics, serve from table

All rolling returns, drawdown, and CAGR distributions are computed after each
sync and stored in the `analytics` table. API queries are pure table lookups.

### Why Pre-compute

| Concern | Pre-compute | On-demand |
|---|---|---|
| API response time | <5ms (index lookup) | 200-2000ms (full NAV scan) |
| CPU at query time | None | High |
| Data freshness | Slightly stale | Always fresh |
| Complexity | Compute pipeline needed | Simpler but slow |

The assignment requires <200ms API response time. On-demand computation over
3000+ NAV points with sliding windows would routinely exceed this. Pre-compute
is the right call.

### Recomputation Trigger

Analytics are recomputed:
1. After every backfill completes
2. After every incremental daily sync
3. On `POST /sync/trigger` (manual)

Since we only have 7-10 funds, full recomputation takes <100ms — cheap enough
to do after every sync.

---

## 5. Handling Schemes with Insufficient History

### Problem
Some schemes are newer and don't have enough history for longer windows.
For example, a fund launched in 2021 cannot have 5Y or 10Y rolling returns.

### Decision: Skip silently, log warning

```go
if len(nav) < windowDays {
    log.Printf("⚠️  Insufficient history for %s window %s", code, label)
    return nil
}
```

The `analytics` table simply won't have a row for that (fund, window) pair.
The API returns a 404 with a clear message rather than returning zeros or
partial data, which would be misleading to investors.

### Forward-Fill for Weekend/Holiday Gaps

NAV data has no entries for weekends and market holidays. The sliding window
needs a continuous daily series to compute correctly.

Decision: **Forward-fill** — carry the last known NAV forward into gap days.

Monday   NAV = 100   → stored
Tuesday  NAV = 102   → stored
Wednesday (holiday)  → filled with 102
Thursday NAV = 105   → stored

This is the standard approach in finance (last-price carry-forward). It does
not distort returns because the filled days cancel out in the return
calculation — a flat period simply contributes a ~0% return for those days.

---

## 6. Analytics Correctness Notes

### Rolling Return Definition
For a window of N years ending on day T:
rolling_return = (NAV[T] - NAV[T - N365]) / NAV[T - N365] * 100

This is the **cumulative total return** over the window, not annualised.
The CAGR field provides the annualised equivalent:
CAGR = ((NAV[T] / NAV[T - N*365]) ^ (1/N) - 1) * 100

### Max Drawdown Definition
Peak-to-trough decline across the **entire NAV history** (not just the window):

drawdown[T] = (NAV[T] - peak[T]) / peak[T] * 100
max_drawdown = min(drawdown) across all T

This gives the worst loss an investor could have experienced from peak to
trough at any point in the fund's history.

---

## 7. Caching Strategy

### Decision: No additional cache layer

The `analytics` table IS the cache. Pre-computed rows are read by indexed
primary key `(scheme_code, window_period)` — this is a single B-tree lookup
which PostgreSQL executes in <5ms even without any application-level cache.

Adding Redis or in-memory caching would add operational complexity with no
meaningful latency improvement given our data volume.

If the system scaled to thousands of funds with high query concurrency,
an in-memory LRU cache with a 1-minute TTL would be the next step.

---

## 8. Evolving Beyond the Assignment

The system is designed to answer questions beyond basic ranking:

**"How did funds perform post-COVID?"**
→ Query `nav_data` for any fund between 2020-02-01 and 2020-06-30 and compute
  drawdown for that specific window. The raw NAV table makes this possible.

**"Has there been an impact on returns post a specific event?"**
→ Add a `GET /funds/{code}/nav?from=2020-01-01&to=2020-12-31` endpoint that
  returns raw NAV data for any date range. The analytics engine can be
  extended to accept arbitrary start/end dates rather than fixed windows.

**"Which fund recovered fastest after a crash?"**
→ Compute time-to-recovery metric: days from drawdown trough back to previous
  peak. This is computable from the existing `nav_data` table with no
  schema changes.

**Scaling to more funds/AMCs:**
→ Add entries to `targetAMCs` and `targetCategories` in `orchestrator.go`.
  The discovery + backfill pipeline handles the rest automatically within
  rate limits.