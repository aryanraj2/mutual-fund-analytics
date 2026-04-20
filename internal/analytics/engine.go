// internal/analytics/engine.go
package analytics

import (
	"context"
	"log"
	"math"
	"mutual-fund-analytics/internal/store"
	"sort"
	"time"
)

// Windows maps label → number of years
var Windows = map[string]int{
	"1Y":  1,
	"3Y":  3,
	"5Y":  5,
	"10Y": 10,
}

type Engine struct {
	db *store.DB
}

func NewEngine(db *store.DB) *Engine {
	return &Engine{db: db}
}

// Result holds all computed analytics for one fund + window
type Result struct {
	SchemeCode      string
	Window          string
	RollingMin      float64
	RollingMax      float64
	RollingMedian   float64
	RollingP25      float64
	RollingP75      float64
	MaxDrawdown     float64
	CAGRMin         float64
	CAGRMax         float64
	CAGRMedian      float64
	PeriodsAnalyzed int
	DataStart       time.Time
	DataEnd         time.Time
	TotalDays       int
	NAVDataPoints   int
	ComputedAt      time.Time
}

// ComputeAll computes analytics for all funds × all windows and persists results.
func (e *Engine) ComputeAll(ctx context.Context) error {
	funds, err := e.db.ListFunds(ctx, "", "")
	if err != nil {
		return err
	}

	for _, fund := range funds {
		nav, err := e.db.GetNAVHistory(ctx, fund.SchemeCode)
		if err != nil {
			log.Printf("⚠️  Could not load NAV for %s: %v", fund.SchemeCode, err)
			continue
		}
		if len(nav) < 2 {
			log.Printf("⚠️  Insufficient NAV data for %s, skipping", fund.SchemeCode)
			continue
		}

		// Forward-fill gaps so sliding window math works on continuous data
		filled := forwardFill(nav)

		for label, years := range Windows {
			result := compute(fund.SchemeCode, label, years, filled)
			if result == nil {
				log.Printf("⚠️  Insufficient history for %s window %s", fund.SchemeCode, label)
				continue
			}
			if err := e.db.UpsertAnalytics(ctx, toStoreAnalytics(*result)); err != nil {
				log.Printf("⚠️  Failed to persist analytics %s/%s: %v", fund.SchemeCode, label, err)
			} else {
				log.Printf("✅ Computed %s | %s | periods=%d", fund.SchemeCode, label, result.PeriodsAnalyzed)
			}
		}
	}
	return nil
}

// compute runs the analytics for a single fund + window.
// Returns nil if the fund doesn't have enough history.
func compute(code, label string, years int, nav []store.NAVPoint) *Result {
	windowDays := years * 365

	if len(nav) < windowDays {
		return nil
	}

	var rollingReturns []float64
	var cagrs []float64
	maxDrawdown := 0.0
	peak := nav[0].Value

	// Slide the window one day at a time
	for i := windowDays; i < len(nav); i++ {
		start := nav[i-windowDays]
		end := nav[i]

		// Rolling return over the window period
		ret := (end.Value - start.Value) / start.Value * 100
		rollingReturns = append(rollingReturns, ret)

		// CAGR = (end/start)^(1/years) - 1
		cagr := (math.Pow(end.Value/start.Value, 1.0/float64(years)) - 1) * 100
		cagrs = append(cagrs, cagr)
	}

	// Max drawdown — track peak across entire NAV history
	for _, p := range nav {
		if p.Value > peak {
			peak = p.Value
		}
		drawdown := (p.Value - peak) / peak * 100
		if drawdown < maxDrawdown {
			maxDrawdown = drawdown
		}
	}

	sort.Float64s(rollingReturns)
	sort.Float64s(cagrs)

	return &Result{
		SchemeCode:      code,
		Window:          label,
		RollingMin:      rollingReturns[0],
		RollingMax:      rollingReturns[len(rollingReturns)-1],
		RollingMedian:   percentile(rollingReturns, 50),
		RollingP25:      percentile(rollingReturns, 25),
		RollingP75:      percentile(rollingReturns, 75),
		MaxDrawdown:     maxDrawdown,
		CAGRMin:         cagrs[0],
		CAGRMax:         cagrs[len(cagrs)-1],
		CAGRMedian:      percentile(cagrs, 50),
		PeriodsAnalyzed: len(rollingReturns),
		DataStart:       nav[0].Date,
		DataEnd:         nav[len(nav)-1].Date,
		TotalDays:       int(nav[len(nav)-1].Date.Sub(nav[0].Date).Hours() / 24),
		NAVDataPoints:   len(nav),
		ComputedAt:      time.Now(),
	}
}

// forwardFill fills weekend/holiday gaps by carrying the last known NAV forward.
// This gives us a continuous daily series for the sliding window.
func forwardFill(nav []store.NAVPoint) []store.NAVPoint {
	if len(nav) == 0 {
		return nav
	}

	var filled []store.NAVPoint
	current := nav[0]
	navIdx := 0

	start := nav[0].Date
	end := nav[len(nav)-1].Date

	for d := start; !d.After(end); d = d.AddDate(0, 0, 1) {
		// Advance to next real data point if available on this date
		if navIdx < len(nav) && sameDay(nav[navIdx].Date, d) {
			current = nav[navIdx]
			navIdx++
		}
		filled = append(filled, store.NAVPoint{
			SchemeCode: current.SchemeCode,
			Date:       d,
			Value:      current.Value,
		})
	}

	return filled
}

func sameDay(a, b time.Time) bool {
	ay, am, ad := a.Date()
	by, bm, bd := b.Date()
	return ay == by && am == bm && ad == bd
}

// percentile returns the p-th percentile of a sorted slice.
func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := (p / 100) * float64(len(sorted)-1)
	lower := int(math.Floor(idx))
	upper := int(math.Ceil(idx))
	if lower == upper {
		return sorted[lower]
	}
	// Linear interpolation
	return sorted[lower] + (idx-float64(lower))*(sorted[upper]-sorted[lower])
}

// toStoreAnalytics converts analytics.Result to store.Analytics
func toStoreAnalytics(r Result) store.Analytics {
	return store.Analytics{
		SchemeCode:      r.SchemeCode,
		WindowPeriod:    r.Window,
		RollingMin:      r.RollingMin,
		RollingMax:      r.RollingMax,
		RollingMedian:   r.RollingMedian,
		RollingP25:      r.RollingP25,
		RollingP75:      r.RollingP75,
		MaxDrawdown:     r.MaxDrawdown,
		CAGRMin:         r.CAGRMin,
		CAGRMax:         r.CAGRMax,
		CAGRMedian:      r.CAGRMedian,
		PeriodsAnalyzed: r.PeriodsAnalyzed,
		DataStart:       r.DataStart,
		DataEnd:         r.DataEnd,
		TotalDays:       r.TotalDays,
		NAVDataPoints:   r.NAVDataPoints,
		ComputedAt:      r.ComputedAt,
	}
}