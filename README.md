# Mutual Fund Analytics Service

A Go backend service that ingests live NAV data from [mfapi.in](https://api.mfapi.in),
computes performance analytics (rolling returns, drawdown, CAGR), and serves
fast ranking and analytics queries via a REST API.

---

## Tech Stack

- **Language:** Go 1.21+
- **Database:** PostgreSQL
- **External API:** [mfapi.in](https://api.mfapi.in) (live, no mocking)
- **Rate Limiter:** Sliding window (2/sec, 50/min, 300/hour)

---

## Tracked Funds

10 schemes across 5 AMCs and 2 categories:

| AMC | Mid Cap | Small Cap |
|---|---|---|
| HDFC Mutual Fund | HDFC Mid Cap Fund | HDFC Small Cap Fund |
| Axis Mutual Fund | Axis Midcap Fund | Axis Small Cap Fund |
| ICICI Prudential | ICICI Prudential MidCap Fund | ICICI Prudential Smallcap Fund |
| Kotak Mahindra | Kotak Midcap Fund | Kotak Small Cap Fund |
| SBI Mutual Fund | SBI Midcap Fund | SBI Small Cap Fund |

---

## Project Structure

mutual-fund-analytics/
├── cmd/
│   └── server/
│       └── main.go              # Entry point
├── internal/
│   ├── api/
│   │   ├── handler.go           # HTTP handlers
│   │   └── routes.go            # Route registration
│   ├── analytics/
│   │   └── engine.go            # Rolling returns, drawdown, CAGR
│   ├── config/
│   │   └── config.go            # Environment config loader
│   ├── mfapi/
│   │   └── client.go            # mfapi.in HTTP client
│   ├── pipeline/
│   │   ├── orchestrator.go      # Backfill + incremental sync
│   │   ├── backfill.go          # Resumable backfill logic
│   │   └── sync.go              # Daily sync scheduler
│   ├── ratelimiter/
│   │   └── sliding_window.go    # 3-constraint rate limiter
│   └── store/
│       ├── db.go                # DB connection
│       ├── funds.go             # Fund queries
│       ├── nav.go               # NAV queries
│       └── analytics.go         # Analytics queries
├── migrations/
│   └── 001_init.sql             # DB schema
├── DESIGN_DECISIONS.md          # Architecture decisions
├── .env                         # Local environment (not committed)
├── go.mod
└── go.sum

---

## Prerequisites

- Go 1.21+
- PostgreSQL 14+

Check versions:
```bash
go version
psql --version
```

---

## Setup & Run

### 1. Clone the repository

```bash
git clone https://github.com/your-username/mutual-fund-analytics.git
cd mutual-fund-analytics
```

### 2. Create the database

```bash
psql -U postgres -c "CREATE DATABASE mf_analytics;"
```

If your PostgreSQL runs on a non-default port (e.g. 5433):
```bash
psql -U postgres -p 5433 -c "CREATE DATABASE mf_analytics;"
```

### 3. Run migrations

```bash
psql -U postgres -p 5433 -d mf_analytics -f migrations/001_init.sql
```

### 4. Configure environment

Create a `.env` file at the project root:

```env
DB_HOST=localhost
DB_PORT=5433
DB_USER=postgres
DB_PASSWORD=your_password
DB_NAME=mf_analytics
DB_SSLMODE=disable
SERVER_PORT=8080
```

### 5. Install dependencies

```bash
go mod tidy
```

### 6. Run the server

```bash
go run cmd/server/main.go
```

On first run the server will:
1. Discover all 10 target schemes from mfapi.in
2. Backfill full NAV history for each scheme
3. Compute analytics for all windows (1Y/3Y/5Y/10Y)
4. Start the HTTP server on port 8080
5. Schedule daily incremental sync

Expected output:
✅ Connected to PostgreSQL
🚀 Starting backfill...
🔍 Starting scheme discovery...
📋 Total schemes from API: 37576
🎯 Matched target schemes: 10
✅ Backfilled 118989 — 3270 NAV points
...
✅ Backfill complete
📊 Computing analytics...
✅ Computed 118989 | 3Y | periods=3760
...
⏰ Daily sync scheduler started
🌐 Server listening on :8080

---

## API Reference

### GET /funds
List all tracked funds. Supports optional filters.

```bash
# All funds
curl http://localhost:8080/funds

# Filter by AMC
curl "http://localhost:8080/funds?amc=HDFC"

# Filter by category
curl "http://localhost:8080/funds?category=Small+Cap"
```

**Response:**
```json
[
  {
    "scheme_code": "118989",
    "scheme_name": "HDFC Mid Cap Fund - Growth Option - Direct Plan",
    "amc": "HDFC Mutual Fund",
    "category": "Equity Scheme - Mid Cap Fund",
    "discovered_at": "2026-04-19T16:55:55.334725+05:30"
  }
]
```

---

### GET /funds/{code}
Get fund details and latest NAV.

```bash
curl http://localhost:8080/funds/118989
```

**Response:**
```json
{
  "fund": {
    "scheme_code": "118989",
    "scheme_name": "HDFC Mid Cap Fund - Growth Option - Direct Plan",
    "amc": "HDFC Mutual Fund",
    "category": "Equity Scheme - Mid Cap Fund"
  },
  "latest_nav": {
    "scheme_code": "118989",
    "nav_date": "2026-04-17",
    "nav_value": 220.058
  }
}
```

---

### GET /funds/{code}/analytics
Get pre-computed analytics for a fund.

**Required query param:** `window` — one of `1Y`, `3Y`, `5Y`, `10Y`

```bash
curl "http://localhost:8080/funds/118989/analytics?window=3Y"
```

**Response:**
```json
{
  "fund_code": "118989",
  "fund_name": "HDFC Mid Cap Fund - Growth Option - Direct Plan",
  "amc": "HDFC Mutual Fund",
  "category": "Equity Scheme - Mid Cap Fund",
  "window": "3Y",
  "data_availability": {
    "start_date": "2013-01-01",
    "end_date": "2026-04-17",
    "total_days": 4854,
    "nav_data_points": 3270
  },
  "rolling_periods_analyzed": 3760,
  "rolling_returns": {
    "min": -23.47,
    "max": 194.85,
    "median": 88.16,
    "p25": 50.01,
    "p75": 117.82
  },
  "max_drawdown": -39.51,
  "cagr": {
    "min": -8.53,
    "max": 43.39,
    "median": 23.45
  },
  "computed_at": "2026-04-19T16:55:58Z"
}
```

---

### GET /funds/rank
Rank funds by performance within a category.

**Required params:** `category`, `window`
**Optional params:** `sort_by` (default: `median_return`), `limit` (default: `5`)

```bash
# Rank Mid Cap funds by median return over 3Y
curl "http://localhost:8080/funds/rank?category=Mid+Cap&window=3Y&sort_by=median_return&limit=5"

# Rank Small Cap funds by max drawdown over 5Y
curl "http://localhost:8080/funds/rank?category=Small+Cap&window=5Y&sort_by=max_drawdown&limit=5"
```

**Response:**
```json
{
  "category": "Mid Cap",
  "window": "3Y",
  "sorted_by": "median_return",
  "total_funds": 5,
  "showing": 5,
  "funds": [
    {
      "rank": 1,
      "fund_code": "118989",
      "fund_name": "HDFC Mid Cap Fund - Growth Option - Direct Plan",
      "amc": "HDFC Mutual Fund",
      "median_return": 88.16,
      "max_drawdown": -39.51,
      "current_nav": 220.058,
      "last_updated": "2026-04-17"
    }
  ]
}
```

---

### POST /sync/trigger
Manually trigger an incremental data sync.

```bash
curl -X POST http://localhost:8080/sync/trigger
```

**Response:**
```json
{
  "status": "triggered",
  "message": "incremental sync started in background"
}
```

---

### GET /sync/status
Check the status of sync jobs.

```bash
curl http://localhost:8080/sync/status
```

**Response:**
```json
{
  "jobs": [
    {
      "id": 1,
      "job_type": "backfill",
      "scheme_code": "118989",
      "status": "done",
      "created_at": "2026-04-19T16:55:55Z"
    }
  ]
}
```

---

## Run Tests

```bash
go test ./... -v
```

Expected:
✅ 22/22 tests passing across 3 packages
--- analytics:    8 tests
--- api:          7 tests
--- ratelimiter:  7 tests

---

## Key Design Decisions

See [DESIGN_DECISIONS.md](./DESIGN_DECISIONS.md) for detailed reasoning on:
- Rate limiting strategy (sliding window)
- Backfill orchestration
- Storage schema
- Pre-computation vs on-demand analytics
- Failure handling and resumability

---

## Scheme Discovery Logic

The service auto-discovers target schemes from mfapi.in's full scheme list
(~37,000 schemes) using keyword matching on fund names. It picks exactly
**one mid cap and one small cap** per AMC using a canonical AMC mapping to
handle naming inconsistencies (e.g. "Kotak" vs "Kotak Mahindra").
