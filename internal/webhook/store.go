// Package webhook implements webhook subscriptions for event-driven automation.
package webhook

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
)

// ErrNotFound is returned when a webhook is not found.
var ErrNotFound = errors.New("not found")

// Webhook represents a webhook subscription.
type Webhook struct {
	ID         string    `json:"id"`
	Name       string    `json:"name"`
	URL        string    `json:"url"`
	Secret     string    `json:"-"`
	EventTypes []string  `json:"event_types"`
	Filter     Filter    `json:"filter"`
	Active     bool      `json:"active"`
	CreatedAt  time.Time `json:"created_at"`
	UpdatedAt  time.Time `json:"updated_at"`
}

// Filter defines conditions for when a webhook should fire.
type Filter struct {
	ProjectSlug string `json:"project_slug,omitempty"`
	Tag         string `json:"tag,omitempty"`
}

// Delivery records a single webhook delivery attempt.
type Delivery struct {
	ID         string    `json:"id"`
	WebhookID  string    `json:"webhook_id"`
	EventType  string    `json:"event_type"`
	Payload    string    `json:"payload"`
	StatusCode int       `json:"status_code,omitempty"`
	Response   string    `json:"response,omitempty"`
	Error      string    `json:"error,omitempty"`
	DurationMs int64     `json:"duration_ms"`
	CreatedAt  time.Time `json:"created_at"`
}

// DBTX is satisfied by both *sql.DB and *sql.Tx.
type DBTX interface {
	ExecContext(ctx context.Context, query string, args ...interface{}) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...interface{}) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...interface{}) *sql.Row
}

// Store provides data access methods for webhooks.
type Store struct{}

// NewStore creates a new webhook Store.
func NewStore() *Store {
	return &Store{}
}

// Create inserts a new webhook.
func (s *Store) Create(ctx context.Context, db DBTX, w *Webhook) error {
	eventTypes := strings.Join(w.EventTypes, ",")
	filterJSON, err := json.Marshal(w.Filter)
	if err != nil {
		return fmt.Errorf("webhook.Store.Create: marshal filter: %w", err)
	}

	active := 0
	if w.Active {
		active = 1
	}

	_, err = db.ExecContext(ctx,
		`INSERT INTO webhooks (id, name, url, secret, event_types, filter, active, created_at, updated_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		w.ID, w.Name, w.URL, w.Secret, eventTypes, string(filterJSON),
		active, w.CreatedAt.Format(time.RFC3339), w.UpdatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("webhook.Store.Create: %w", err)
	}
	return nil
}

// Get retrieves a single webhook by ID.
func (s *Store) Get(ctx context.Context, db DBTX, id string) (*Webhook, error) {
	row := db.QueryRowContext(ctx,
		`SELECT id, name, url, secret, event_types, filter, active, created_at, updated_at
		 FROM webhooks WHERE id = ?`, id)

	w, err := scanWebhook(row)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, fmt.Errorf("webhook.Store.Get: %w", ErrNotFound)
		}
		return nil, fmt.Errorf("webhook.Store.Get: %w", err)
	}
	return w, nil
}

// List returns all webhooks, optionally filtering to active only.
func (s *Store) List(ctx context.Context, db DBTX, activeOnly bool) ([]*Webhook, error) {
	query := `SELECT id, name, url, secret, event_types, filter, active, created_at, updated_at FROM webhooks`
	if activeOnly {
		query += ` WHERE active = 1`
	}
	query += ` ORDER BY created_at DESC`

	rows, err := db.QueryContext(ctx, query)
	if err != nil {
		return nil, fmt.Errorf("webhook.Store.List: %w", err)
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w, err := scanWebhookRow(rows)
		if err != nil {
			return nil, fmt.Errorf("webhook.Store.List: scan: %w", err)
		}
		webhooks = append(webhooks, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook.Store.List: rows: %w", err)
	}
	return webhooks, nil
}

// Update updates a webhook by ID.
func (s *Store) Update(ctx context.Context, db DBTX, w *Webhook) error {
	eventTypes := strings.Join(w.EventTypes, ",")
	filterJSON, err := json.Marshal(w.Filter)
	if err != nil {
		return fmt.Errorf("webhook.Store.Update: marshal filter: %w", err)
	}

	active := 0
	if w.Active {
		active = 1
	}

	result, err := db.ExecContext(ctx,
		`UPDATE webhooks SET name = ?, url = ?, event_types = ?, filter = ?, active = ?, updated_at = ?
		 WHERE id = ?`,
		w.Name, w.URL, eventTypes, string(filterJSON), active,
		w.UpdatedAt.Format(time.RFC3339), w.ID,
	)
	if err != nil {
		return fmt.Errorf("webhook.Store.Update: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook.Store.Update: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("webhook.Store.Update: %w", ErrNotFound)
	}
	return nil
}

// Delete removes a webhook by ID.
func (s *Store) Delete(ctx context.Context, db DBTX, id string) error {
	result, err := db.ExecContext(ctx, `DELETE FROM webhooks WHERE id = ?`, id)
	if err != nil {
		return fmt.Errorf("webhook.Store.Delete: %w", err)
	}

	rows, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("webhook.Store.Delete: rows affected: %w", err)
	}
	if rows == 0 {
		return fmt.Errorf("webhook.Store.Delete: %w", ErrNotFound)
	}
	return nil
}

// ListByEvent returns active webhooks whose event_types contain the given event type.
func (s *Store) ListByEvent(ctx context.Context, db DBTX, eventType string) ([]*Webhook, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT id, name, url, secret, event_types, filter, active, created_at, updated_at
		 FROM webhooks
		 WHERE active = 1 AND (',' || event_types || ',') LIKE '%,' || ? || ',%'`,
		eventType,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook.Store.ListByEvent: %w", err)
	}
	defer rows.Close()

	var webhooks []*Webhook
	for rows.Next() {
		w, err := scanWebhookRow(rows)
		if err != nil {
			return nil, fmt.Errorf("webhook.Store.ListByEvent: scan: %w", err)
		}
		webhooks = append(webhooks, w)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook.Store.ListByEvent: rows: %w", err)
	}
	return webhooks, nil
}

// CreateDelivery inserts a delivery record.
func (s *Store) CreateDelivery(ctx context.Context, db DBTX, d *Delivery) error {
	_, err := db.ExecContext(ctx,
		`INSERT INTO webhook_deliveries (id, webhook_id, event_type, payload, status_code, response, error, duration_ms, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		d.ID, d.WebhookID, d.EventType, d.Payload,
		d.StatusCode, d.Response, d.Error, d.DurationMs,
		d.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("webhook.Store.CreateDelivery: %w", err)
	}
	return nil
}

// ListDeliveries returns recent deliveries for a webhook.
func (s *Store) ListDeliveries(ctx context.Context, db DBTX, webhookID string, limit int) ([]*Delivery, error) {
	if limit <= 0 {
		limit = 20
	}

	rows, err := db.QueryContext(ctx,
		`SELECT id, webhook_id, event_type, payload, status_code, response, error, duration_ms, created_at
		 FROM webhook_deliveries
		 WHERE webhook_id = ?
		 ORDER BY created_at DESC
		 LIMIT ?`,
		webhookID, limit,
	)
	if err != nil {
		return nil, fmt.Errorf("webhook.Store.ListDeliveries: %w", err)
	}
	defer rows.Close()

	var deliveries []*Delivery
	for rows.Next() {
		d := &Delivery{}
		var createdAt string
		var statusCode sql.NullInt64
		var response, errStr sql.NullString
		if err := rows.Scan(&d.ID, &d.WebhookID, &d.EventType, &d.Payload,
			&statusCode, &response, &errStr, &d.DurationMs, &createdAt); err != nil {
			return nil, fmt.Errorf("webhook.Store.ListDeliveries: scan: %w", err)
		}
		if statusCode.Valid {
			d.StatusCode = int(statusCode.Int64)
		}
		if response.Valid {
			d.Response = response.String
		}
		if errStr.Valid {
			d.Error = errStr.String
		}
		d.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
		deliveries = append(deliveries, d)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("webhook.Store.ListDeliveries: rows: %w", err)
	}
	return deliveries, nil
}

// scanWebhook scans a single webhook from a *sql.Row.
func scanWebhook(row *sql.Row) (*Webhook, error) {
	w := &Webhook{}
	var eventTypes, filterJSON, createdAt, updatedAt string
	var active int

	if err := row.Scan(&w.ID, &w.Name, &w.URL, &w.Secret,
		&eventTypes, &filterJSON, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	w.EventTypes = splitEventTypes(eventTypes)
	w.Active = active == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	_ = json.Unmarshal([]byte(filterJSON), &w.Filter)

	return w, nil
}

// scanWebhookRow scans a single webhook from *sql.Rows.
func scanWebhookRow(rows *sql.Rows) (*Webhook, error) {
	w := &Webhook{}
	var eventTypes, filterJSON, createdAt, updatedAt string
	var active int

	if err := rows.Scan(&w.ID, &w.Name, &w.URL, &w.Secret,
		&eventTypes, &filterJSON, &active, &createdAt, &updatedAt); err != nil {
		return nil, err
	}

	w.EventTypes = splitEventTypes(eventTypes)
	w.Active = active == 1
	w.CreatedAt, _ = time.Parse(time.RFC3339, createdAt)
	w.UpdatedAt, _ = time.Parse(time.RFC3339, updatedAt)
	_ = json.Unmarshal([]byte(filterJSON), &w.Filter)

	return w, nil
}

// splitEventTypes splits a comma-separated event type string into a slice.
func splitEventTypes(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	result := make([]string, 0, len(parts))
	for _, p := range parts {
		p = strings.TrimSpace(p)
		if p != "" {
			result = append(result, p)
		}
	}
	return result
}
