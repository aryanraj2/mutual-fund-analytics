// internal/analytics/engine_test.go
package analytics

import (
	"math"
	"mutual-fund-analytics/internal/store"
	"testing"
	"time"
)

// --- Percentile tests ---

func TestPercentileMedian(t *testing.T) {
	data := []float64{1, 2, 3, 4, 5}
	got := percentile(data, 50)
	if got != 3.0 {
		t.Errorf("expected median=3.0, got %v", got)
	}
	t.Logf("✅ Percentile median: %v", got)
}

func TestPercentileP25P75(t *testing.T) {
	data := []float64{10, 20, 30, 40, 50, 60, 70, 80, 90, 100}
	p25 := percentile(data, 25)
	p75 := percentile(data, 75)

	if p25 < 25 || p25 > 35 {
		t.Errorf("p25 out of expected range, got %v", p25)
	}
	if p75 < 65 || p75 > 80 {
		t.Errorf("p75 out of expected range, got %v", p75)
	}
	t.Logf("✅ p25=%v p75=%v", p25, p75)
}

func TestPercentileEmpty(t *testing.T) {
	got := percentile([]float64{}, 50)
	if got != 0 {
		t.Errorf("expected 0 for empty slice, got %v", got)
	}
	t.Log("✅ Percentile handles empty slice")
}

// --- Forward fill tests ---

func TestForwardFillFillsGaps(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Monday and Wednesday only — Tuesday is missing
	nav := []store.NAVPoint{
		{SchemeCode: "TEST", Date: base, Value: 100},
		{SchemeCode: "TEST", Date: base.AddDate(0, 0, 2), Value: 102},
	}

	filled := forwardFill(nav)

	// Should have 3 days: Mon, Tue (filled), Wed
	if len(filled) != 3 {
		t.Errorf("expected 3 points after fill, got %d", len(filled))
	}

	// Tuesday should carry Monday's value
	tuesday := filled[1]
	if tuesday.Value != 100 {
		t.Errorf("Tuesday should be forward-filled with 100, got %v", tuesday.Value)
	}
	t.Logf("✅ Forward fill: %v points, Tuesday filled with %v", len(filled), tuesday.Value)
}

func TestForwardFillNoGaps(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)
	nav := []store.NAVPoint{
		{SchemeCode: "TEST", Date: base, Value: 100},
		{SchemeCode: "TEST", Date: base.AddDate(0, 0, 1), Value: 101},
		{SchemeCode: "TEST", Date: base.AddDate(0, 0, 2), Value: 102},
	}

	filled := forwardFill(nav)
	if len(filled) != 3 {
		t.Errorf("expected 3 points with no gaps, got %d", len(filled))
	}
	t.Log("✅ Forward fill: no gaps, no extra points added")
}

// --- Rolling return and CAGR manual verification ---

func TestComputeRollingReturns(t *testing.T) {
	// Build 2 years of flat NAV data so we can verify math precisely
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	var nav []store.NAVPoint

	for i := 0; i < 800; i++ {
		// NAV grows linearly from 100 to 180 over 800 days
		val := 100.0 + float64(i)*0.1
		nav = append(nav, store.NAVPoint{
			SchemeCode: "TEST",
			Date:       base.AddDate(0, 0, i),
			Value:      val,
		})
	}

	result := compute("TEST", "1Y", 1, nav)
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	// With linear growth, all rolling returns should be positive
	if result.RollingMin < 0 {
		t.Errorf("expected positive rolling min for growing NAV, got %v", result.RollingMin)
	}

	// CAGR should be positive
	if result.CAGRMedian < 0 {
		t.Errorf("expected positive CAGR for growing NAV, got %v", result.CAGRMedian)
	}

	// Max drawdown should be 0 (NAV never falls)
	if result.MaxDrawdown < -0.001 {
		t.Errorf("expected ~0 drawdown for monotonically growing NAV, got %v", result.MaxDrawdown)
	}

	t.Logf("✅ Rolling returns: min=%.2f max=%.2f median=%.2f",
		result.RollingMin, result.RollingMax, result.RollingMedian)
	t.Logf("✅ CAGR: min=%.2f max=%.2f median=%.2f",
		result.CAGRMin, result.CAGRMax, result.CAGRMedian)
}

func TestComputeMaxDrawdown(t *testing.T) {
	base := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)

	// NAV goes 100 → 200 → 100 (50% drawdown)
	nav := []store.NAVPoint{}
	for i := 0; i < 400; i++ {
		var val float64
		if i < 200 {
			val = 100 + float64(i)*0.5 // rises to 200
		} else {
			val = 200 - float64(i-200)*0.5 // falls back to 100
		}
		nav = append(nav, store.NAVPoint{
			SchemeCode: "TEST",
			Date:       base.AddDate(0, 0, i),
			Value:      val,
		})
	}

	result := compute("TEST", "1Y", 1, nav)
	if result == nil {
		t.Fatal("expected result, got nil")
	}

	// Max drawdown should be close to -50%
	expected := -50.0
	if math.Abs(result.MaxDrawdown-expected) > 2.0 {
		t.Errorf("expected drawdown ~-50%%, got %.2f", result.MaxDrawdown)
	}
	t.Logf("✅ Max drawdown correctly computed: %.2f%%", result.MaxDrawdown)
}

func TestInsufficientHistory(t *testing.T) {
	base := time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)

	// Only 100 days of data — not enough for 1Y (365 days)
	var nav []store.NAVPoint
	for i := 0; i < 100; i++ {
		nav = append(nav, store.NAVPoint{
			SchemeCode: "TEST",
			Date:       base.AddDate(0, 0, i),
			Value:      100 + float64(i),
		})
	}

	result := compute("TEST", "1Y", 1, nav)
	if result != nil {
		t.Error("expected nil result for insufficient history")
	}
	t.Log("✅ Insufficient history correctly returns nil")
}