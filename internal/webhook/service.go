package webhook

import (
	"context"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

// Sentinel errors for validation failures.
var (
	ErrInvalidURL       = fmt.Errorf("invalid webhook URL")
	ErrInvalidEventType = fmt.Errorf("invalid event type")
	ErrNameRequired     = fmt.Errorf("name is required")
	ErrURLRequired      = fmt.Errorf("url is required")
	ErrEventsRequired   = fmt.Errorf("event_types is required")
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
	secretBytes := make([]byte, 32)
	if _, err := rand.Read(secretBytes); err != nil {
		return nil, fmt.Errorf("webhook.Service.Create: generate secret: %w", err)
	}

	w := &Webhook{
		ID:         ulid.MustNew(ulid.Now(), rand.Reader).String(),
		Name:       strings.TrimSpace(req.Name),
		URL:        strings.TrimSpace(req.URL),
		Secret:     hex.EncodeToString(secretBytes),
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
		// Use a background context since the caller does not wait.
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

		var wg sync.WaitGroup
		for _, wh := range webhooks {
			// Check filter match.
			if !s.matchesFilter(wh.Filter, eventPayload) {
				continue
			}

			wg.Add(1)
			go func(wh *Webhook) {
				defer wg.Done()
				s.deliver(bgCtx, db, wh, eventType, payloadJSON)
			}(wh)
		}
		wg.Wait()
	}()
}

// deliver sends the webhook HTTP request and records the delivery.
func (s *Service) deliver(ctx context.Context, db DBTX, wh *Webhook, eventType string, payloadJSON []byte) {
	start := time.Now()

	// Compute HMAC-SHA256 signature.
	mac := hmac.New(sha256.New, []byte(wh.Secret))
	mac.Write(payloadJSON)
	signature := "sha256=" + hex.EncodeToString(mac.Sum(nil))

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, wh.URL, strings.NewReader(string(payloadJSON)))
	if err != nil {
		s.recordDelivery(ctx, db, wh.ID, eventType, string(payloadJSON), 0, "", err.Error(), time.Since(start))
		return
	}

	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Seam-Signature", signature)
	req.Header.Set("X-Seam-Event", eventType)

	resp, err := s.client.Do(req)
	if err != nil {
		s.recordDelivery(ctx, db, wh.ID, eventType, string(payloadJSON), 0, "", err.Error(), time.Since(start))
		s.logger.Warn("webhook delivery failed",
			"webhook_id", wh.ID, "url", wh.URL, "error", err)
		return
	}
	defer resp.Body.Close()

	// Read response body (limited).
	body, _ := io.ReadAll(io.LimitReader(resp.Body, maxResponseBody))
	respText := string(body)

	duration := time.Since(start)

	var errText string
	if resp.StatusCode >= 400 {
		errText = fmt.Sprintf("HTTP %d", resp.StatusCode)
	}

	s.recordDelivery(ctx, db, wh.ID, eventType, string(payloadJSON), resp.StatusCode, respText, errText, duration)

	s.logger.Info("webhook delivered",
		"webhook_id", wh.ID,
		"url", wh.URL,
		"status_code", resp.StatusCode,
		"duration_ms", duration.Milliseconds())
}

// recordDelivery persists a delivery record.
func (s *Service) recordDelivery(ctx context.Context, db DBTX, webhookID, eventType, payload string, statusCode int, response, errText string, duration time.Duration) {
	d := &Delivery{
		ID:         ulid.MustNew(ulid.Now(), rand.Reader).String(),
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
		return true // cannot inspect, let it through
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

// validateWebhookURL checks that a URL is well-formed and uses http or https.
func validateWebhookURL(rawURL string) error {
	parsed, err := url.Parse(rawURL)
	if err != nil {
		return fmt.Errorf("%w: %v", ErrInvalidURL, err)
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

// isPrivateIP returns true if the host resolves to a private/loopback IP.
func isPrivateIP(host string) bool {
	ip := net.ParseIP(host)
	if ip == nil {
		addrs, err := net.LookupHost(host)
		if err != nil || len(addrs) == 0 {
			return true // fail closed
		}
		ip = net.ParseIP(addrs[0])
	}
	if ip == nil {
		return true
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() || ip.IsLinkLocalMulticast()
}
