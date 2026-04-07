package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/userdb"
)

// Supported event types.
const (
	EventNoteCreated  = "note.created"
	EventNoteModified = "note.modified"
	EventNoteDeleted  = "note.deleted"
	EventTaskComplete = "task.complete"
	EventTaskFailed   = "task.failed"
)

// AllEventTypes lists all subscribable events.
var AllEventTypes = []string{
	EventNoteCreated, EventNoteModified, EventNoteDeleted,
	EventTaskComplete, EventTaskFailed,
}

// maxResponseBody is the max bytes to store from webhook response.
const maxResponseBody = 1024

// deliveryTimeout is the HTTP timeout for webhook delivery.
const deliveryTimeout = 10 * time.Second

// maxConcurrentDeliveries limits how many webhook deliveries can be in-flight
// simultaneously, preventing resource exhaustion during bulk operations.
const maxConcurrentDeliveries = 20

// Sentinel errors for validation failures.
var (
	ErrInvalidURL       = errors.New("invalid webhook URL")
	ErrInvalidEventType = errors.New("invalid event type")
	ErrNameRequired     = errors.New("name is required")
	ErrURLRequired      = errors.New("url is required")
	ErrEventsRequired   = errors.New("event_types is required")
)

// CreateReq holds parameters for creating a webhook.
type CreateReq struct {
	Name       string   `json:"name"`
	URL        string   `json:"url"`
	EventTypes []string `json:"event_types"`
	Filter     Filter   `json:"filter,omitempty"`
}

// UpdateReq holds parameters for updating a webhook.
type UpdateReq struct {
	Name       *string   `json:"name,omitempty"`
	URL        *string   `json:"url,omitempty"`
	EventTypes *[]string `json:"event_types,omitempty"`
	Filter     *Filter   `json:"filter,omitempty"`
	Active     *bool     `json:"active,omitempty"`
}

// deliveryResult captures the outcome of a single webhook delivery attempt.
type deliveryResult struct {
	webhookID  string
	eventType  string
	payload    string
	statusCode int
	response   string
	errText    string
	duration   time.Duration
}

// Service provides business logic for webhook management and delivery.
type Service struct {
	store     *Store
	dbManager userdb.Manager
	client    *http.Client
	logger    *slog.Logger
}

// NewService creates a new webhook Service.
func NewService(store *Store, dbManager userdb.Manager, logger *slog.Logger) *Service {
	if logger == nil {
		logger = slog.Default()
	}
	return &Service{
		store:     store,
		dbManager: dbManager,
		client: &http.Client{
			Timeout: deliveryTimeout,
			CheckRedirect: func(req *http.Request, via []*http.Request) error {
				// Do not follow redirects to private IPs.
				host := req.URL.Hostname()
				if isPrivateIP(host) {
					return fmt.Errorf("webhook.Service: redirect to private IP blocked")
				}
				if len(via) >= 5 {
					return fmt.Errorf("webhook.Service: too many redirects")
				}
				return nil
			},
		},
		logger: logger,
	}
}

// Create validates and creates a new webhook subscription.
func (s *Service) Create(ctx context.Context, userID string, req CreateReq) (*Webhook, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("webhook.Service.Create: %w", ErrNameRequired)
	}
	if strings.TrimSpace(req.URL) == "" {
		return nil, fmt.Errorf("webhook.Service.Create: %w", ErrURLRequired)
	}
	if len(req.EventTypes) == 0 {
		return nil, fmt.Errorf("webhook.Service.Create: %w", ErrEventsRequired)
	}

	// Validate URL.
	if err := validateWebhookURL(req.URL); err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: %w", err)
	}

	// Validate event types.
	for _, et := range req.EventTypes {
		if !isValidEventType(et) {
			return nil, fmt.Errorf("webhook.Service.Create: %w: %s", ErrInvalidEventType, et)
		}
	}

	// Warn about private IPs but do not block (local-first app).
	parsed, _ := url.Parse(req.URL)
	if parsed != nil {
		host := parsed.Hostname()
		if isPrivateIP(host) {
			s.logger.Warn("webhook URL points to private IP",
				"url", req.URL, "host", host, "user_id", userID)
		}
	}

	// Generate ULID and random secret.
	now := time.Now().UTC()
	secret, err := generateSecret()
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: %w", err)
	}

	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: generate id: %w", err)
	}

	w := &Webhook{
		ID:         id.String(),
		Name:       strings.TrimSpace(req.Name),
		URL:        strings.TrimSpace(req.URL),
		Secret:     secret,
		EventTypes: req.EventTypes,
		Filter:     req.Filter,
		Active:     true,
		CreatedAt:  now,
		UpdatedAt:  now,
	}

	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: %w", err)
	}

	if err := s.store.Create(ctx, db, w); err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: %w", err)
	}

	s.logger.Info("webhook created",
		"id", w.ID, "name", w.Name, "user_id", userID,
		"event_types", strings.Join(w.EventTypes, ","))

	return w, nil
}

// Get retrieves a single webhook.
func (s *Service) Get(ctx context.Context, userID, id string) (*Webhook, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Get: %w", err)
	}
	w, err := s.store.Get(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Get: %w", err)
	}
	return w, nil
}

// List returns webhooks for a user.
func (s *Service) List(ctx context.Context, userID string, activeOnly bool) ([]*Webhook, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.List: %w", err)
	}
	webhooks, err := s.store.List(ctx, db, activeOnly)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.List: %w", err)
	}
	return webhooks, nil
}

// Update modifies an existing webhook.
func (s *Service) Update(ctx context.Context, userID, id string, req UpdateReq) (*Webhook, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Update: %w", err)
	}

	w, err := s.store.Get(ctx, db, id)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Update: %w", err)
	}

	if req.Name != nil {
		name := strings.TrimSpace(*req.Name)
		if name == "" {
			return nil, fmt.Errorf("webhook.Service.Update: %w", ErrNameRequired)
		}
		w.Name = name
	}
	if req.URL != nil {
		u := strings.TrimSpace(*req.URL)
		if u == "" {
			return nil, fmt.Errorf("webhook.Service.Update: %w", ErrURLRequired)
		}
		if err := validateWebhookURL(u); err != nil {
			return nil, fmt.Errorf("webhook.Service.Update: %w", err)
		}
		w.URL = u
	}
	if req.EventTypes != nil {
		if len(*req.EventTypes) == 0 {
			return nil, fmt.Errorf("webhook.Service.Update: %w", ErrEventsRequired)
		}
		for _, et := range *req.EventTypes {
			if !isValidEventType(et) {
				return nil, fmt.Errorf("webhook.Service.Update: %w: %s", ErrInvalidEventType, et)
			}
		}
		w.EventTypes = *req.EventTypes
	}
	if req.Filter != nil {
		w.Filter = *req.Filter
	}
	if req.Active != nil {
		w.Active = *req.Active
	}

	w.UpdatedAt = time.Now().UTC()

	if err := s.store.Update(ctx, db, w); err != nil {
		return nil, fmt.Errorf("webhook.Service.Update: %w", err)
	}

	return w, nil
}

// RotateSecret generates a new HMAC signing secret for a webhook and returns it.
func (s *Service) RotateSecret(ctx context.Context, userID, id string) (string, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return "", fmt.Errorf("webhook.Service.RotateSecret: open db: %w", err)
	}

	secret, err := generateSecret()
	if err != nil {
		return "", fmt.Errorf("webhook.Service.RotateSecret: %w", err)
	}

	if err := s.store.UpdateSecret(ctx, db, id, secret); err != nil {
		return "", fmt.Errorf("webhook.Service.RotateSecret: %w", err)
	}

	return secret, nil
}

// Delete removes a webhook.
func (s *Service) Delete(ctx context.Context, userID, id string) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("webhook.Service.Delete: %w", err)
	}
	if err := s.store.Delete(ctx, db, id); err != nil {
		return fmt.Errorf("webhook.Service.Delete: %w", err)
	}
	s.logger.Info("webhook deleted", "id", id, "user_id", userID)
	return nil
}

// Deliveries returns recent delivery records for a webhook.
func (s *Service) Deliveries(ctx context.Context, userID, webhookID string, limit int) ([]*Delivery, error) {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Deliveries: %w", err)
	}
	deliveries, err := s.store.ListDeliveries(ctx, db, webhookID, limit)
	if err != nil {
		return nil, fmt.Errorf("webhook.Service.Deliveries: %w", err)
	}
	return deliveries, nil
}

// Dispatch fires webhooks matching the given event type. It is fire-and-forget:
// deliveries run in goroutines and results are logged, not returned.
func (s *Service) Dispatch(ctx context.Context, userID, eventType string, eventPayload interface{}) {
	go func() {
		defer func() {
			if r := recover(); r != nil {
				s.logger.Error("webhook.Service.Dispatch: panic recovered",
					"panic", r, "user_id", userID, "event_type", eventType)
			}
		}()

		if !isValidEventType(eventType) {
			s.logger.Warn("webhook.Service.Dispatch: invalid event type",
				"event_type", eventType)
			return
		}

		bgCtx := context.Background()

		db, err := s.dbManager.Open(bgCtx, userID)
		if err != nil {
			s.logger.Error("webhook.Service.Dispatch: open db",
				"user_id", userID, "error", err)
			return
		}

		webhooks, err := s.store.ListByEvent(bgCtx, db, eventType)
		if err != nil {
			s.logger.Error("webhook.Service.Dispatch: list by event",
				"user_id", userID, "event_type", eventType, "error", err)
			return
		}

		if len(webhooks) == 0 {
			return
		}

		// Build the event payload JSON.
		payloadData := map[string]interface{}{
			"event_type": eventType,
			"timestamp":  time.Now().UTC().Format(time.RFC3339),
			"data":       eventPayload,
		}
		payloadJSON, err := json.Marshal(payloadData)
		if err != nil {
			s.logger.Error("webhook.Service.Dispatch: marshal payload",
				"error", err)
			return
		}

		results := make(chan deliveryResult, len(webhooks))
		sem := make(chan struct{}, maxConcurrentDeliveries)
		var wg sync.WaitGroup
		for _, wh := range webhooks {
			if !s.matchesFilter(wh.Filter, eventPayload) {
				continue
			}

			wg.Add(1)
			go func(wh *Webhook) {
				defer wg.Done()
				defer func() {
					if r := recover(); r != nil {
						s.logger.Error("webhook.Service.deliver: panic recovered",
							"panic", r, "webhook_id", wh.ID)
						results <- deliveryResult{
							webhookID: wh.ID,
							eventType: eventType,
							payload:   string(payloadJSON),
							errText:   fmt.Sprintf("panic: %v", r),
						}
					}
				}()
				sem <- struct{}{}        // acquire slot
				defer func() { <-sem }() // release slot
				results <- s.deliver(bgCtx, wh, eventType, payloadJSON)
			}(wh)
		}
		wg.Wait()
		close(results)

		// Record all deliveries sequentially to avoid concurrent SQLite writes.
		for dr := range results {
			s.recordDelivery(bgCtx, db, dr.webhookID, dr.eventType, dr.payload,
				dr.statusCode, dr.response, dr.errText, dr.duration)
		}

		// Opportunistic cleanup: remove delivery records older than 30 days.
		if cleanupErr := s.CleanupDeliveries(bgCtx, userID, 30*24*time.Hour); cleanupErr != nil {
			s.logger.Warn("webhook.Service.Dispatch: cleanup failed", "error", cleanupErr)
		}
	}()
}

// deliver sends the webhook HTTP request and returns the delivery result.
func (s *Service) deliver(ctx context.Context, wh *Webhook, eventType string, payloadJSON []byte) deliveryResult {
	start := time.Now()
	payload := string(payloadJSON)

	// SSRF defense: check target URL for private IPs before delivery.
	parsed, parseErr := url.Parse(wh.URL)
	if parseErr != nil {
		return deliveryResult{
			webhookID: wh.ID, eventType: eventType, payload: string(payloadJSON),
			errText: "invalid URL: " + parseErr.Error(), duration: time.Since(start),
		}
	}
	if isPrivateIP(parsed.Hostname()) {
		s.logger.Debug("webhook delivery to private IP",
			"webhook_id", wh.ID, "url", wh.URL)
	}

	// Compute HMAC-SHA256 signature.
	mac := hmac.New(sha256.New, []byte(wh.Secret))
	mac.Write(payloadJSON)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(payload))
	if err != nil {
		return deliveryResult{
			webhookID: wh.ID, eventType: eventType, payload: payload,
			errText: err.Error(), duration: time.Since(start),
		}
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Seam-Signature", signature)
	req.Header.Set("X-Seam-Event", eventType)

	resp, err := s.client.Do(req)
	if err != nil {
		s.logger.Warn("webhook delivery failed",
			"webhook_id", wh.ID, "url", wh.URL, "error", err)
		return deliveryResult{
			webhookID: wh.ID, eventType: eventType, payload: payload,
			errText: err.Error(), duration: time.Since(start),
		}
	}
	defer resp.Body.Close()

	// Read response body (limited) and drain the remainder so the
	// HTTP connection can be reused by the connection pool.
	body, readErr := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	if readErr != nil {
		s.logger.Debug("webhook response body read error",
			"webhook_id", wh.ID, "error", readErr)
	}
	io.Copy(io.Discard, resp.Body) //nolint:errcheck // best-effort drain
	respText := string(body)

	duration := time.Since(start)

	var errText string
	if resp.StatusCode >= 400 {
		errText = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	s.logger.Info("webhook delivered",
		"webhook_id", wh.ID,
		"url", wh.URL,
		"status_code", resp.StatusCode,
		"duration_ms", duration.Milliseconds())

	return deliveryResult{
		webhookID:  wh.ID,
		eventType:  eventType,
		payload:    payload,
		statusCode: resp.StatusCode,
		response:   respText,
		errText:    errText,
		duration:   duration,
	}
}

// recordDelivery persists a delivery record.
func (s *Service) recordDelivery(ctx context.Context, db DBTX, webhookID, eventType, payload string, statusCode int, response, errText string, duration time.Duration) {
	id, err := ulid.New(ulid.Now(), rand.Reader)
	if err != nil {
		s.logger.Error("webhook.Service.recordDelivery: generate id", "error", err)
		return
	}

	d := &Delivery{
		ID:         id.String(),
		WebhookID:  webhookID,
		EventType:  eventType,
		Payload:    payload,
		StatusCode: statusCode,
		Response:   response,
		Error:      errText,
		DurationMs: duration.Milliseconds(),
		CreatedAt:  time.Now().UTC(),
	}

	if err := s.store.CreateDelivery(ctx, db, d); err != nil {
		s.logger.Error("webhook.Service.recordDelivery: failed to record delivery",
			"webhook_id", webhookID, "error", err)
	}
}

// matchesFilter checks whether the event payload matches the webhook filter.
func (s *Service) matchesFilter(f Filter, eventPayload interface{}) bool {
	if f.ProjectSlug == "" && f.Tag == "" {
		return true // no filter, always matches
	}

	// Try to extract project_slug and tags from the payload.
	data, ok := eventPayload.(map[string]interface{})
	if !ok {
		// Try JSON round-trip for struct payloads.
		b, jsonErr := json.Marshal(eventPayload)
		if jsonErr != nil {
			return true // cannot inspect, let it through
		}
		if jsonErr := json.Unmarshal(b, &data); jsonErr != nil {
			return true
		}
	}

	if f.ProjectSlug != "" {
		slug, _ := data["project_slug"].(string)
		if slug != f.ProjectSlug {
			return false
		}
	}

	if f.Tag != "" {
		tags, _ := data["tags"].([]string)
		if tags == nil {
			// Try []interface{} (from JSON unmarshaling).
			if rawTags, ok := data["tags"].([]interface{}); ok {
				for _, rt := range rawTags {
					if t, ok := rt.(string); ok {
						tags = append(tags, t)
					}
				}
			}
		}
		found := false
		for _, t := range tags {
			if t == f.Tag {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return true
}

// CleanupDeliveries removes delivery records older than the given retention period.
func (s *Service) CleanupDeliveries(ctx context.Context, userID string, retention time.Duration) error {
	db, err := s.dbManager.Open(ctx, userID)
	if err != nil {
		return fmt.Errorf("webhook.Service.CleanupDeliveries: open db: %w", err)
	}
	cutoff := time.Now().UTC().Add(-retention).Format(time.RFC3339)
	_, err = db.ExecContext(ctx, `DELETE FROM webhook_deliveries WHERE created_at < ?`, cutoff)
	if err != nil {
		return fmt.Errorf("webhook.Service.CleanupDeliveries: %w", err)
	}
	return nil
}

// generateSecret returns a new random hex-encoded 32-byte HMAC signing secret.
func generateSecret() (string, error) {
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		return "", fmt.Errorf("generate secret: %w", err)
	}
	return hex.EncodeToString(b), nil
}

// validateWebhookURL checks that a URL is well-formed and uses http or https.
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %w", ErrInvalidURL, err)
	}
	if parsed.Scheme != "http" && parsed.Scheme != "https" {
		return fmt.Errorf("%w: scheme must be http or https", ErrInvalidURL)
	}
	if parsed.Host == "" {
		return fmt.Errorf("%w: host is required", ErrInvalidURL)
	}
	return nil
}

// isValidEventType checks if the given event type is in the AllEventTypes list.
func isValidEventType(et string) bool {
	for _, valid := range AllEventTypes {
		if et == valid {
			return true
		}
	}
	return false
}

func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return true // fail closed
		}
		// Check ALL resolved addresses, not just the first.
		for _, addr := range addrs {
			resolved := net.ParseIP(addr)
			if resolved != nil && isDangerousIP(resolved) {
				return true
			}
		}
		return false
	}
	return isDangerousIP(ip)
}

// isDangerousIP checks if an IP address is in a non-routable or dangerous range.
func isDangerousIP(ip net.IP) bool {
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified() || ip.IsMulticast()
}
