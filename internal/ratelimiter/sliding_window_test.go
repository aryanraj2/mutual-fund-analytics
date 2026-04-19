// internal/ratelimiter/sliding_window_test.go
package ratelimiter

import (
	"context"
	"database/sql"
	"sync"
	"testing"
	"time"

	_ "github.com/lib/pq"
)

// newTestLimiter creates a limiter with a real DB for persistence tests
// and a nil DB for pure in-memory tests
func newTestLimiter(db *sql.DB) *SlidingWindowLimiter {
	return &SlidingWindowLimiter{
		db:           db,
		secondWindow: []time.Time{},
		minuteWindow: []time.Time{},
		hourWindow:   []time.Time{},
	}
}

// --- Per-second limit tests ---

func TestPerSecondLimit(t *testing.T) {
	l := newTestLimiter(nil)
	ctx := context.Background()

	// First 2 requests should pass immediately
	start := time.Now()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("first request failed: %v", err)
	}
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("second request failed: %v", err)
	}

	// Both passed under 100ms — no blocking happened
	if time.Since(start) > 100*time.Millisecond {
		t.Errorf("first two requests should be instant, took %v", time.Since(start))
	}

	// Third request must block until the second window expires
	start3 := time.Now()
	if err := l.Wait(ctx); err != nil {
		t.Fatalf("third request failed: %v", err)
	}
	elapsed := time.Since(start3)
	if elapsed < 500*time.Millisecond {
		t.Errorf("third request should have waited ~1s for per-second limit, waited %v", elapsed)
	}
	t.Logf("✅ Per-second limit: third request waited %v", elapsed)
}

// --- Per-minute limit tests ---

func TestPerMinuteLimitNotExceeded(t *testing.T) {
	l := newTestLimiter(nil)

	// Manually fill minute window to just under the limit
	now := time.Now()
	for i := 0; i < perMinuteLimit-1; i++ {
		// spread across last 59 seconds so they're all still in window
		ts := now.Add(-time.Duration(i) * time.Second / 2)
		l.minuteWindow = append(l.minuteWindow, ts)
		l.hourWindow = append(l.hourWindow, ts)
	}

	// Should not block since we're 1 under the limit
	ctx := context.Background()
	done := make(chan error, 1)
	go func() {
		done <- l.Wait(ctx)
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		t.Log("✅ Per-minute limit: request passed when under limit")
	case <-time.After(500 * time.Millisecond):
		t.Error("request should not have blocked when under per-minute limit")
	}
}

// --- Per-hour limit tests ---

func TestPerHourLimitBlocks(t *testing.T) {
	l := newTestLimiter(nil)

	// Fill hour window to exactly the limit
	now := time.Now()
	for i := 0; i < perHourLimit; i++ {
		ts := now.Add(-time.Duration(i) * time.Second)
		l.hourWindow = append(l.hourWindow, ts)
	}
	// Also fill second and minute windows to avoid those blocking first
	l.secondWindow = []time.Time{}
	l.minuteWindow = []time.Time{}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	err := l.Wait(ctx)
	if err == nil {
		t.Error("expected context timeout when hour limit is full, got nil")
	} else {
		t.Logf("✅ Per-hour limit blocks correctly: %v", err)
	}
}

// --- Concurrent access tests ---

func TestConcurrentAccess(t *testing.T) {
	l := newTestLimiter(nil)
	ctx := context.Background()

	var wg sync.WaitGroup
	errors := make(chan error, 10)

	// Fire 5 goroutines simultaneously
	for i := 0; i < 5; i++ {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			if err := l.Wait(ctx); err != nil {
				errors <- err
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	for err := range errors {
		t.Errorf("concurrent request failed: %v", err)
	}

	// Verify window sizes are consistent — no double-counting
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.secondWindow) > perSecondLimit {
		t.Errorf("second window has %d entries, max is %d", len(l.secondWindow), perSecondLimit)
	}
	t.Logf("✅ Concurrent access: second=%d minute=%d hour=%d",
		len(l.secondWindow), len(l.minuteWindow), len(l.hourWindow))
}

// --- Context cancellation ---

func TestContextCancellation(t *testing.T) {
	l := newTestLimiter(nil)

	// Fill all windows so every request blocks
	now := time.Now()
	for i := 0; i < perSecondLimit; i++ {
		l.secondWindow = append(l.secondWindow, now)
	}

	ctx, cancel := context.WithCancel(context.Background())

	done := make(chan error, 1)
	go func() {
		done <- l.Wait(ctx)
	}()

	// Cancel after 50ms
	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Error("expected error after context cancellation")
		}
		t.Logf("✅ Context cancellation works: %v", err)
	case <-time.After(2 * time.Second):
		t.Error("Wait did not respect context cancellation")
	}
}

// --- Prune test ---

func TestPruneRemovesExpiredEntries(t *testing.T) {
	l := newTestLimiter(nil)

	// Add timestamps that are already expired
	old := time.Now().Add(-2 * time.Second)
	l.secondWindow = []time.Time{old, old}
	l.minuteWindow = []time.Time{old, old}

	l.prune(time.Now())

	if len(l.secondWindow) != 0 {
		t.Errorf("expected 0 entries after prune, got %d", len(l.secondWindow))
	}
	t.Log("✅ Prune correctly removes expired entries")
}

// --- Percentile test (used in analytics) ---

func TestWaitTimeCalculation(t *testing.T) {
	now := time.Now()

	// Window has 2 entries (at limit), oldest is 500ms ago
	window := []time.Time{
		now.Add(-500 * time.Millisecond),
		now.Add(-100 * time.Millisecond),
	}

	wait := waitTime(window, 2, time.Second, now)

	// Should wait ~500ms (1s - 500ms elapsed)
	if wait < 400*time.Millisecond || wait > 600*time.Millisecond {
		t.Errorf("expected ~500ms wait, got %v", wait)
	}
	t.Logf("✅ Wait time calculation correct: %v", wait)
}