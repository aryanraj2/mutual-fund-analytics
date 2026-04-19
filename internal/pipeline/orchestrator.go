// internal/pipeline/orchestrator.go
package pipeline

import (
	"context"
	"log"
	"mutual-fund-analytics/internal/mfapi"
	"mutual-fund-analytics/internal/ratelimiter"
	"mutual-fund-analytics/internal/store"
	"strings"
	"time"
)

var targetAMCs = []string{
	"icici prudential",
	"hdfc",
	"axis",
	"sbi",
	"kotak mahindra",
}

var targetCategories = []string{
	"mid cap",
	"small cap",
}

type Orchestrator struct {
	db      *store.DB
	client  *mfapi.Client
	limiter *ratelimiter.SlidingWindowLimiter
}

func NewOrchestrator(db *store.DB, client *mfapi.Client, limiter *ratelimiter.SlidingWindowLimiter) *Orchestrator {
	return &Orchestrator{db: db, client: client, limiter: limiter}
}

func (o *Orchestrator) Backfill(ctx context.Context) error {
	log.Println("🔍 Starting scheme discovery...")

	if err := o.limiter.Wait(ctx); err != nil {
		return err
	}
	allSchemes, err := o.client.FetchAllSchemes(ctx)
	if err != nil {
		return err
	}
	log.Printf("📋 Total schemes from API: %d", len(allSchemes))

	targeted := o.filterSchemes(allSchemes)
	log.Printf("🎯 Matched target schemes: %d", len(targeted))

	for _, scheme := range targeted {
		if err := o.backfillScheme(ctx, scheme.Code); err != nil {
			log.Printf("⚠️  Failed to backfill %s (%s): %v", scheme.Code, scheme.Name, err)
			o.markJobFailed(ctx, scheme.Code, err)
			continue
		}
	}

	log.Println("✅ Backfill complete")
	return nil
}

func (o *Orchestrator) IncrementalSync(ctx context.Context) error {
	log.Println("🔄 Starting incremental sync...")

	funds, err := o.db.ListFunds(ctx, "", "")
	if err != nil {
		return err
	}

	for _, fund := range funds {
		if err := o.limiter.Wait(ctx); err != nil {
			return err
		}

		detail, err := o.client.FetchSchemeDetail(ctx, fund.SchemeCode)
		if err != nil {
			log.Printf("⚠️  Incremental sync failed for %s: %v", fund.SchemeCode, err)
			continue
		}

		points := toNAVPoints(fund.SchemeCode, detail.History)
		if len(points) > 5 {
			points = points[len(points)-5:]
		}

		if err := o.db.BulkUpsertNAV(ctx, points); err != nil {
			log.Printf("⚠️  Failed to upsert NAV for %s: %v", fund.SchemeCode, err)
		}

		log.Printf("✅ Synced %s", fund.SchemeCode)
	}
	return nil
}

func (o *Orchestrator) backfillScheme(ctx context.Context, code string) error {
	o.createJob(ctx, code, "backfill", "running")

	if err := o.limiter.Wait(ctx); err != nil {
		return err
	}

	detail, err := o.client.FetchSchemeDetail(ctx, code)
	if err != nil {
		return err
	}

	if err := o.db.UpsertFund(ctx, store.Fund{
		SchemeCode: code,
		SchemeName: detail.Info.Name,
		AMC:        detail.Info.FundHouse,
		Category:   detail.Info.Category,
	}); err != nil {
		return err
	}

	points := toNAVPoints(code, detail.History)
	if err := o.db.BulkUpsertNAV(ctx, points); err != nil {
		return err
	}

	log.Printf("✅ Backfilled %s — %d NAV points", code, len(points))
	o.markJobDone(ctx, code)
	return nil
}

func (o *Orchestrator) filterSchemes(all []mfapi.SchemeInfo) []mfapi.SchemeInfo {
	var matched []mfapi.SchemeInfo
	for _, s := range all {
		nameLower := strings.ToLower(s.Name)
		if !matchesAny(nameLower, targetAMCs) {
			continue
		}
		if !matchesAny(nameLower, targetCategories) {
			continue
		}
		if !strings.Contains(nameLower, "direct") {
			continue
		}
		if !strings.Contains(nameLower, "growth") {
			continue
		}
		matched = append(matched, s)
	}
	return matched
}

func matchesAny(s string, keywords []string) bool {
	for _, kw := range keywords {
		if strings.Contains(s, kw) {
			return true
		}
	}
	return false
}

func toNAVPoints(code string, history []mfapi.NAVEntry) []store.NAVPoint {
	points := make([]store.NAVPoint, 0, len(history))
	for _, h := range history {
		points = append(points, store.NAVPoint{
			SchemeCode: code,
			Date:       h.Date,
			Value:      h.Value,
		})
	}
	return points
}

func (o *Orchestrator) createJob(ctx context.Context, code, jobType, status string) {
	o.db.Conn.ExecContext(ctx, `
		INSERT INTO sync_jobs (job_type, scheme_code, status, started_at)
		VALUES ($1, $2, $3, NOW())
	`, jobType, code, status)
}

func (o *Orchestrator) markJobDone(ctx context.Context, code string) {
	o.db.Conn.ExecContext(ctx, `
		UPDATE sync_jobs SET status='done', completed_at=NOW()
		WHERE scheme_code=$1 AND status='running'
	`, code)
}

func (o *Orchestrator) markJobFailed(ctx context.Context, code string, err error) {
	o.db.Conn.ExecContext(ctx, `
		UPDATE sync_jobs SET status='failed', completed_at=NOW(), error_msg=$2
		WHERE scheme_code=$1 AND status='running'
	`, code, err.Error())
}

func (o *Orchestrator) SyncStatus(ctx context.Context) ([]SyncJobStatus, error) {
	rows, err := o.db.Conn.QueryContext(ctx, `
		SELECT id, job_type, scheme_code, status, started_at, completed_at, error_msg, created_at
		FROM sync_jobs
		ORDER BY created_at DESC
		LIMIT 50
	`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var jobs []SyncJobStatus
	for rows.Next() {
		var j SyncJobStatus
		var schemeCode, errorMsg *string
		var startedAt, completedAt *time.Time
		if err := rows.Scan(
			&j.ID, &j.JobType, &schemeCode, &j.Status,
			&startedAt, &completedAt, &errorMsg, &j.CreatedAt,
		); err != nil {
			return nil, err
		}
		if schemeCode != nil {
			j.SchemeCode = *schemeCode
		}
		if errorMsg != nil {
			j.ErrorMsg = *errorMsg
		}
		jobs = append(jobs, j)
	}
	return jobs, nil
}

type SyncJobStatus struct {
	ID         int       `json:"id"`
	JobType    string    `json:"job_type"`
	SchemeCode string    `json:"scheme_code"`
	Status     string    `json:"status"`
	ErrorMsg   string    `json:"error_msg,omitempty"`
	CreatedAt  time.Time `json:"created_at"`
}