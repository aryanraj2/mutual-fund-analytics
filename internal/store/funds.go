// internal/store/funds.go
package store

import (
	"context"
	"database/sql"
	"time"
)

type Fund struct {
	SchemeCode   string    `json:"scheme_code"`
	SchemeName   string    `json:"scheme_name"`
	AMC          string    `json:"amc"`
	Category     string    `json:"category"`
	DiscoveredAt time.Time `json:"discovered_at"`
}

func (db *DB) UpsertFund(ctx context.Context, f Fund) error {
	query := `
		INSERT INTO funds (scheme_code, scheme_name, amc, category, discovered_at)
		VALUES ($1, $2, $3, $4, NOW())
		ON CONFLICT (scheme_code) DO UPDATE
			SET scheme_name = EXCLUDED.scheme_name,
				amc         = EXCLUDED.amc,
				category    = EXCLUDED.category
	`
	_, err := db.Conn.ExecContext(ctx, query,
		f.SchemeCode, f.SchemeName, f.AMC, f.Category,
	)
	return err
}

func (db *DB) ListFunds(ctx context.Context, amc, category string) ([]Fund, error) {
	query := `
		SELECT scheme_code, scheme_name, amc, category, discovered_at
		FROM funds
		WHERE ($1 = '' OR amc ILIKE '%' || $1 || '%')
		  AND ($2 = '' OR category ILIKE '%' || $2 || '%')
		ORDER BY amc, scheme_name
	`
	rows, err := db.Conn.QueryContext(ctx, query, amc, category)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var funds []Fund
	for rows.Next() {
		var f Fund
		if err := rows.Scan(
			&f.SchemeCode, &f.SchemeName, &f.AMC,
			&f.Category, &f.DiscoveredAt,
		); err != nil {
			return nil, err
		}
		funds = append(funds, f)
	}
	return funds, rows.Err()
}

func (db *DB) GetFund(ctx context.Context, code string) (*Fund, error) {
	query := `
		SELECT scheme_code, scheme_name, amc, category, discovered_at
		FROM funds WHERE scheme_code = $1
	`
	var f Fund
	err := db.Conn.QueryRowContext(ctx, query, code).Scan(
		&f.SchemeCode, &f.SchemeName, &f.AMC,
		&f.Category, &f.DiscoveredAt,
	)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	return &f, err
}