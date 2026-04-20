// internal/pipeline/sync.go
package pipeline

import (
	"context"
	"log"
	"time"
)

// StartDailySync runs an incremental sync on a daily ticker.
// Call this in a goroutine from main.
func (o *Orchestrator) StartDailySync(ctx context.Context) {
	ticker := time.NewTicker(24 * time.Hour)
	defer ticker.Stop()

	log.Println("⏰ Daily sync scheduler started")

	for {
		select {
		case <-ctx.Done():
			log.Println("🛑 Daily sync stopped")
			return
		case t := <-ticker.C:
			log.Printf("🔄 Daily sync triggered at %s", t.Format(time.RFC3339))
			if err := o.IncrementalSync(ctx); err != nil {
				log.Printf("⚠️  Daily sync error: %v", err)
			}
		}
	}
}