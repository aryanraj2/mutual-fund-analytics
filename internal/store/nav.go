// internal/store/nav.go
package store

import (
	"context"
	"time"
)

type NAVPoint struct {
	SchemeCode string
	Date       time.Time
	Value      float64
}

// BulkUpsertNAV inserts NAV records, ignoring duplicates.
// Called during backfill — safe to re-run.
func (db *DB) BulkUpsertNAV(ctx context.Context, points []NAVPoint) error {
	if len(points) == 0 {
		return nil
	}

	tx, err := db.Conn.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.PrepareContext(ctx, `
		INSERT INTO nav_data (scheme_code, nav_date, nav_value)
		VALUES ($1, $2, $3)
		ON CONFLICT (scheme_code, nav_date) DO NOTHING
	`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, p := range points {
		if _, err := stmt.ExecContext(ctx, p.SchemeCode, p.Date, p.Value); err != nil {
			return err
		}
	}
	return tx.Commit()
}

// GetNAVHistory returns all NAV points for a scheme, ascending by date.
func (db *DB) GetNAVHistory(ctx context.Context, schemeCode string) ([]NAVPoint, error) {
	rows, err := db.Conn.QueryContext(ctx, `
		SELECT scheme_code, nav_date, nav_value
		FROM nav_data
		WHERE scheme_code = $1
		ORDER BY nav_date ASC
	`, schemeCode)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []NAVPoint
	for rows.Next() {
		var p NAVPoint
		if err := rows.Scan(&p.SchemeCode, &p.Date, &p.Value); err != nil {
			return nil, err
		}
		points = append(points, p)
	}
	return points, rows.Err()
}

// GetLatestNAV returns the most recent NAV entry for a scheme.
func (db *DB) GetLatestNAV(ctx context.Context, schemeCode string) (*NAVPoint, error) {
	var p NAVPoint
	err := db.Conn.QueryRowContext(ctx, `
		SELECT scheme_code, nav_date, nav_value
		FROM nav_data
		WHERE scheme_code = $1
		ORDER BY nav_date DESC
		LIMIT 1
	`, schemeCode).Scan(&p.SchemeCode, &p.Date, &p.Value)
	if err != nil {
		return nil, err
	}
	return &p, nil
}