package usage

import (
	"context"
	"database/sql"
	"fmt"
	"time"
)

// DBTX is satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store provides data access for the token_usage table.
type Store struct{}

// NewStore creates a new usage Store.
func NewStore() *Store { return &Store{} }

// Insert persists a single usage record.
func (s *Store) Insert(ctx context.Context, db DBTX, r *Record) error {
	isLocal := 0
	if r.IsLocal {
		isLocal = 1
	}
	_, err := db.ExecContext(ctx,
		`INSERT INTO token_usage
		 (id, user_id, function, provider, model, input_tokens, output_tokens,
		  total_tokens, is_local, duration_ms, conversation_id, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		r.ID, r.UserID, string(r.Function), r.Provider, r.Model,
		r.InputTokens, r.OutputTokens, r.TotalTokens,
		isLocal, r.DurationMS, nilIfEmpty(r.ConversationID),
		r.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("usage.Store.Insert: %w", err)
	}
	return nil
}

// GetSummary returns aggregated token counts for the given time range.
func (s *Store) GetSummary(ctx context.Context, db DBTX, from, to time.Time) (*Summary, error) {
	row := db.QueryRowContext(ctx,
		`SELECT
		   COALESCE(SUM(total_tokens), 0),
		   COALESCE(SUM(input_tokens), 0),
		   COALESCE(SUM(output_tokens), 0),
		   COALESCE(SUM(CASE WHEN is_local = 0 THEN total_tokens ELSE 0 END), 0),
		   COALESCE(SUM(CASE WHEN is_local = 1 THEN total_tokens ELSE 0 END), 0),
		   COUNT(*)
		 FROM token_usage
		 WHERE created_at >= ? AND created_at <= ?`,
		from.Format(time.RFC3339), to.Format(time.RFC3339),
	)
	var sum Summary
	if err := row.Scan(&sum.TotalTokens, &sum.InputTokens, &sum.OutputTokens,
		&sum.BilledTokens, &sum.LocalTokens, &sum.CallCount); err != nil {
		return nil, fmt.Errorf("usage.Store.GetSummary: %w", err)
	}
	return &sum, nil
}

// GetByFunction returns usage grouped by function for the given time range.
func (s *Store) GetByFunction(ctx context.Context, db DBTX, from, to time.Time) ([]FunctionUsage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT function,
		   COALESCE(SUM(total_tokens), 0),
		   COALESCE(SUM(input_tokens), 0),
		   COALESCE(SUM(output_tokens), 0),
		   COUNT(*)
		 FROM token_usage
		 WHERE created_at >= ? AND created_at <= ?
		 GROUP BY function
		 ORDER BY SUM(total_tokens) DESC`,
		from.Format(time.RFC3339), to.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("usage.Store.GetByFunction: %w", err)
	}
	defer rows.Close()

	var result []FunctionUsage
	for rows.Next() {
		var fu FunctionUsage
		if err := rows.Scan(&fu.Function, &fu.TotalTokens, &fu.InputTokens,
			&fu.OutputTokens, &fu.CallCount); err != nil {
			return nil, fmt.Errorf("usage.Store.GetByFunction: scan: %w", err)
		}
		result = append(result, fu)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage.Store.GetByFunction: rows: %w", err)
	}
	return result, nil
}

// GetByProvider returns usage grouped by provider for the given time range.
func (s *Store) GetByProvider(ctx context.Context, db DBTX, from, to time.Time) ([]ProviderUsage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT provider,
		   COALESCE(SUM(total_tokens), 0),
		   COALESCE(SUM(input_tokens), 0),
		   COALESCE(SUM(output_tokens), 0),
		   COUNT(*)
		 FROM token_usage
		 WHERE created_at >= ? AND created_at <= ?
		 GROUP BY provider
		 ORDER BY SUM(total_tokens) DESC`,
		from.Format(time.RFC3339), to.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("usage.Store.GetByProvider: %w", err)
	}
	defer rows.Close()

	var result []ProviderUsage
	for rows.Next() {
		var pu ProviderUsage
		if err := rows.Scan(&pu.Provider, &pu.TotalTokens, &pu.InputTokens,
			&pu.OutputTokens, &pu.CallCount); err != nil {
			return nil, fmt.Errorf("usage.Store.GetByProvider: scan: %w", err)
		}
		result = append(result, pu)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage.Store.GetByProvider: rows: %w", err)
	}
	return result, nil
}

// GetByModel returns usage grouped by model (with provider) for the given time range.
func (s *Store) GetByModel(ctx context.Context, db DBTX, from, to time.Time) ([]ModelUsage, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT model, provider,
		   COALESCE(SUM(total_tokens), 0),
		   COALESCE(SUM(input_tokens), 0),
		   COALESCE(SUM(output_tokens), 0),
		   COUNT(*)
		 FROM token_usage
		 WHERE created_at >= ? AND created_at <= ?
		 GROUP BY model, provider
		 ORDER BY SUM(total_tokens) DESC`,
		from.Format(time.RFC3339), to.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("usage.Store.GetByModel: %w", err)
	}
	defer rows.Close()

	var result []ModelUsage
	for rows.Next() {
		var mu ModelUsage
		if err := rows.Scan(&mu.Model, &mu.Provider, &mu.TotalTokens, &mu.InputTokens,
			&mu.OutputTokens, &mu.CallCount); err != nil {
			return nil, fmt.Errorf("usage.Store.GetByModel: scan: %w", err)
		}
		result = append(result, mu)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage.Store.GetByModel: rows: %w", err)
	}
	return result, nil
}

// GetTimeSeries returns usage bucketed by time for the given range.
// Granularity must be "hour", "day", or "month".
func (s *Store) GetTimeSeries(ctx context.Context, db DBTX, from, to time.Time, granularity string) ([]TimeSeriesPoint, error) {
	var bucketExpr string
	switch granularity {
	case "hour":
		bucketExpr = "SUBSTR(created_at, 1, 13)" // "2026-04-09T14"
	case "month":
		bucketExpr = "SUBSTR(created_at, 1, 7)" // "2026-04"
	default: // "day"
		bucketExpr = "SUBSTR(created_at, 1, 10)" // "2026-04-09"
	}

	query := fmt.Sprintf(
		`SELECT %s AS bucket,
		   COALESCE(SUM(total_tokens), 0),
		   COALESCE(SUM(CASE WHEN is_local = 0 THEN total_tokens ELSE 0 END), 0),
		   COUNT(*)
		 FROM token_usage
		 WHERE created_at >= ? AND created_at <= ?
		 GROUP BY bucket
		 ORDER BY bucket`,
		bucketExpr,
	)

	rows, err := db.QueryContext(ctx, query, from.Format(time.RFC3339), to.Format(time.RFC3339))
	if err != nil {
		return nil, fmt.Errorf("usage.Store.GetTimeSeries: %w", err)
	}
	defer rows.Close()

	var result []TimeSeriesPoint
	for rows.Next() {
		var pt TimeSeriesPoint
		if err := rows.Scan(&pt.Bucket, &pt.TotalTokens, &pt.BilledTokens, &pt.CallCount); err != nil {
			return nil, fmt.Errorf("usage.Store.GetTimeSeries: scan: %w", err)
		}
		result = append(result, pt)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage.Store.GetTimeSeries: rows: %w", err)
	}
	return result, nil
}

// GetPeriodTotal returns the total non-local tokens consumed in the current
// period ("daily" or "monthly"). Used for budget enforcement.
func (s *Store) GetPeriodTotal(ctx context.Context, db DBTX, period string, gateLocal bool) (int64, error) {
	now := time.Now().UTC()
	var periodStart time.Time
	switch period {
	case "daily":
		periodStart = time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.UTC)
	default: // "monthly"
		periodStart = time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	}

	query := `SELECT COALESCE(SUM(total_tokens), 0) FROM token_usage WHERE created_at >= ?`
	args := []interface{}{periodStart.Format(time.RFC3339)}

	if !gateLocal {
		query += " AND is_local = 0"
	}

	var total int64
	if err := db.QueryRowContext(ctx, query, args...).Scan(&total); err != nil {
		return 0, fmt.Errorf("usage.Store.GetPeriodTotal: %w", err)
	}
	return total, nil
}

func nilIfEmpty(s string) interface{} {
	if s == "" {
		return nil
	}
	return s
}
