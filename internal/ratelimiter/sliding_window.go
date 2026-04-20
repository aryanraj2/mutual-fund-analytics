// internal/ratelimiter/sliding_window.go
package ratelimiter

import (
	"context"
	"database/sql"
	"fmt"
	"sync"
	"time"
)

const (
	perSecondLimit = 2
	perMinuteLimit = 50
	perHourLimit   = 300
)

type SlidingWindowLimiter struct {
	mu   sync.Mutex
	db   *sql.DB

	secondWindow []time.Time
	minuteWindow []time.Time
	hourWindow   []time.Time
}

func New(db *sql.DB) *SlidingWindowLimiter {
	l := &SlidingWindowLimiter{db: db}
	l.loadFromDB()
	return l
}

func (l *SlidingWindowLimiter) loadFromDB() {
	// Skip if no DB configured (e.g. in tests)
	if l.db == nil {
		return
	}

	rows, err := l.db.Query(`
		SELECT requested_at FROM rate_limiter_log
		WHERE requested_at > NOW() - INTERVAL '1 hour'
		ORDER BY requested_at ASC
	`)
	if err != nil {
		return
	}
	defer rows.Close()

	now := time.Now()
	for rows.Next() {
		var t time.Time
		if err := rows.Scan(&t); err != nil {
			continue
		}
		if now.Sub(t) <= time.Hour {
			l.hourWindow = append(l.hourWindow, t)
		}
		if now.Sub(t) <= time.Minute {
			l.minuteWindow = append(l.minuteWindow, t)
		}
		if now.Sub(t) <= time.Second {
			l.secondWindow = append(l.secondWindow, t)
		}
	}
}

func (l *SlidingWindowLimiter) Wait(ctx context.Context) error {
	for {
		l.mu.Lock()
		waitDur := l.nextAvailable()
		l.mu.Unlock()

		if waitDur == 0 {
			l.mu.Lock()
			l.record(ctx)
			l.mu.Unlock()
			return nil
		}

		select {
		case <-ctx.Done():
			return fmt.Errorf("context cancelled: %w", ctx.Err())
		case <-time.After(waitDur):
		}
	}
}

func (l *SlidingWindowLimiter) nextAvailable() time.Duration {
	now := time.Now()
	l.prune(now)

	if wait := waitTime(l.secondWindow, perSecondLimit, time.Second, now); wait > 0 {
		return wait
	}
	if wait := waitTime(l.minuteWindow, perMinuteLimit, time.Minute, now); wait > 0 {
		return wait
	}
	if wait := waitTime(l.hourWindow, perHourLimit, time.Hour, now); wait > 0 {
		return wait
	}
	return 0
}

func waitTime(window []time.Time, limit int, duration time.Duration, now time.Time) time.Duration {
	if len(window) < limit {
		return 0
	}
	oldest := window[0]
	return oldest.Add(duration).Sub(now)
}

func (l *SlidingWindowLimiter) prune(now time.Time) {
	l.secondWindow = filterRecent(l.secondWindow, now, time.Second)
	l.minuteWindow = filterRecent(l.minuteWindow, now, time.Minute)
	l.hourWindow   = filterRecent(l.hourWindow, now, time.Hour)
}

func filterRecent(window []time.Time, now time.Time, duration time.Duration) []time.Time {
	cutoff := now.Add(-duration)
	i := 0
	for i < len(window) && window[i].Before(cutoff) {
		i++
	}
	return window[i:]
}

func (l *SlidingWindowLimiter) record(ctx context.Context) {
	now := time.Now()
	l.secondWindow = append(l.secondWindow, now)
	l.minuteWindow = append(l.minuteWindow, now)
	l.hourWindow   = append(l.hourWindow, now)

	// Skip DB persistence if no DB is configured (e.g. in tests)
	if l.db == nil {
		return
	}

	go func() {
		l.db.ExecContext(ctx, `INSERT INTO rate_limiter_log (requested_at) VALUES ($1)`, now)
		l.db.ExecContext(ctx, `DELETE FROM rate_limiter_log WHERE requested_at < NOW() - INTERVAL '1 hour'`)
	}()
}