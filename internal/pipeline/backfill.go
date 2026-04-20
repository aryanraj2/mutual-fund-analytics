// internal/pipeline/backfill.go
package pipeline

import (
	"context"
	"log"
)

// RunBackfill is the entry point for a full historical backfill.
// It is idempotent — safe to re-run after a crash.
func (o *Orchestrator) RunBackfill(ctx context.Context) error {
	log.Println("🔁 RunBackfill started")

	// Check which schemes already have data so we can skip them
	existing, err := o.db.ListFunds(ctx, "", "")
	if err != nil {
		return err
	}

	done := make(map[string]bool)
	for _, f := range existing {
		navs, err := o.db.GetNAVHistory(ctx, f.SchemeCode)
		if err == nil && len(navs) > 10 {
			done[f.SchemeCode] = true
			log.Printf("⏭️  Skipping %s — already has %d NAV points", f.SchemeCode, len(navs))
		}
	}

	// Run full backfill — orchestrator.Backfill handles discovery + fetching
	return o.Backfill(ctx)
}