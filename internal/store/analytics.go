// internal/store/analytics.go
package store

import (
	"context"
	"database/sql"
	"time"
)

type Analytics struct {
	SchemeCode      string
	WindowPeriod    string
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

func (db *DB) UpsertAnalytics(ctx context.Context, a Analytics) error {
	_, err := db.Conn.ExecContext(ctx, `
		INSERT INTO analytics (
			scheme_code, window_period,
			rolling_min, rolling_max, rolling_median, rolling_p25, rolling_p75,
			max_drawdown,
			cagr_min, cagr_max, cagr_median,
			periods_analyzed, data_start, data_end,
			total_days, nav_data_points, computed_at
		) VALUES (
			$1,$2,$3,$4,$5,$6,$7,$8,$9,$10,$11,$12,$13,$14,$15,$16,$17
		)
		ON CONFLICT (scheme_code, window_period) DO UPDATE SET
			rolling_min      = EXCLUDED.rolling_min,
			rolling_max      = EXCLUDED.rolling_max,
			rolling_median   = EXCLUDED.rolling_median,
			rolling_p25      = EXCLUDED.rolling_p25,
			rolling_p75      = EXCLUDED.rolling_p75,
			max_drawdown     = EXCLUDED.max_drawdown,
			cagr_min         = EXCLUDED.cagr_min,
			cagr_max         = EXCLUDED.cagr_max,
			cagr_median      = EXCLUDED.cagr_median,
			periods_analyzed = EXCLUDED.periods_analyzed,
			data_start       = EXCLUDED.data_start,
			data_end         = EXCLUDED.data_end,
			total_days       = EXCLUDED.total_days,
			nav_data_points  = EXCLUDED.nav_data_points,
			computed_at      = EXCLUDED.computed_at
	`,
		a.SchemeCode, a.WindowPeriod,
		a.RollingMin, a.RollingMax, a.RollingMedian, a.RollingP25, a.RollingP75,
		a.MaxDrawdown,
		a.CAGRMin, a.CAGRMax, a.CAGRMedian,
		a.PeriodsAnalyzed, a.DataStart, a.DataEnd,
		a.TotalDays, a.NAVDataPoints, a.ComputedAt,
	)
	return err
}

func (db *DB) GetAnalytics(ctx context.Context, schemeCode, window string) (*Analytics, error) {
	var a Analytics
	err := db.Conn.QueryRowContext(ctx, `
		SELECT
			scheme_code, window_period,
			rolling_min, rolling_max, rolling_median, rolling_p25, rolling_p75,
			max_drawdown,
			cagr_min, cagr_max, cagr_median,
			periods_analyzed, data_start, data_end,
			total_days, nav_data_points, computed_at
		FROM analytics
		WHERE scheme_code = $1 AND window_period = $2
	`, schemeCode, window).Scan(
		&a.SchemeCode, &a.WindowPeriod,
		&a.RollingMin, &a.RollingMax, &a.RollingMedian, &a.RollingP25, &a.RollingP75,
		&a.MaxDrawdown,
		&a.CAGRMin, &a.CAGRMax, &a.CAGRMedian,
		&a.PeriodsAnalyzed, &a.DataStart, &a.DataEnd,
		&a.TotalDays, &a.NAVDataPoints, &a.ComputedAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &a, err
}

func (db *DB) GetRankings(ctx context.Context, category, sortBy, window string, limit int) ([]Analytics, error) {
	// sort_by can be 'median_return' or 'max_drawdown'
	orderClause := "a.rolling_median DESC"
	if sortBy == "max_drawdown" {
		orderClause = "a.max_drawdown DESC" // least negative = best
	}

	query := `
		SELECT
			a.scheme_code, a.window_period,
			a.rolling_min, a.rolling_max, a.rolling_median, a.rolling_p25, a.rolling_p75,
			a.max_drawdown,
			a.cagr_min, a.cagr_max, a.cagr_median,
			a.periods_analyzed, a.data_start, a.data_end,
			a.total_days, a.nav_data_points, a.computed_at
		FROM analytics a
		JOIN funds f ON f.scheme_code = a.scheme_code
		WHERE a.window_period = $1
		  AND ($2 = '' OR f.category ILIKE '%' || $2 || '%')
		ORDER BY ` + orderClause + `
		LIMIT $3
	`
	rows, err := db.Conn.QueryContext(ctx, query, window, category, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var results []Analytics
	for rows.Next() {
		var a Analytics
		if err := rows.Scan(
			&a.SchemeCode, &a.WindowPeriod,
			&a.RollingMin, &a.RollingMax, &a.RollingMedian, &a.RollingP25, &a.RollingP75,
			&a.MaxDrawdown,
			&a.CAGRMin, &a.CAGRMax, &a.CAGRMedian,
			&a.PeriodsAnalyzed, &a.DataStart, &a.DataEnd,
			&a.TotalDays, &a.NAVDataPoints, &a.ComputedAt,
		); err != nil {
			return nil, err
		}
		results = append(results, a)
	}
	return results, rows.Err()
}