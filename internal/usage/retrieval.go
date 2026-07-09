package usage

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log/slog"
	"sort"
	"time"

	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

const retrievalQueryMaxRunes = 500

// Retrieval event kinds.
const (
	RetrievalKindBriefing      = "briefing"
	RetrievalKindPromptContext = "prompt_context"
	RetrievalKindRecall        = "recall"
	RetrievalKindMemoryRead    = "memory_read"
)

// readAfterInjectWindow is how long after an injection a memory_read counts as
// a follow-up read of an injected item.
const readAfterInjectWindow = 15 * time.Minute

// RetrievalEvent records one retrieval/injection event for hit-rate analysis.
type RetrievalEvent struct {
	ID          string
	UserID      string
	Kind        string // briefing | prompt_context | recall | memory_read
	ProjectSlug string
	Query       string
	Items       []string // memory keys ("category/name") or note IDs surfaced/read
	Hit         bool
	CreatedAt   time.Time
}

// RetrievalStore is data access for the retrieval_events table.
type RetrievalStore struct{}

// NewRetrievalStore creates a RetrievalStore.
func NewRetrievalStore() *RetrievalStore { return &RetrievalStore{} }

// Insert persists a retrieval event. The query is truncated to 500 runes.
func (s *RetrievalStore) Insert(ctx context.Context, db DBTX, ev *RetrievalEvent) error {
	itemsJSON, err := json.Marshal(ev.Items)
	if err != nil {
		itemsJSON = []byte("[]")
	}
	hit := 0
	if ev.Hit {
		hit = 1
	}
	query := ev.Query
	if r := []rune(query); len(r) > retrievalQueryMaxRunes {
		query = string(r[:retrievalQueryMaxRunes])
	}
	_, err = db.ExecContext(ctx,
		`INSERT INTO retrieval_events (id, user_id, kind, project_slug, query, items, hit, created_at)
		 VALUES (?, ?, ?, ?, ?, ?, ?, ?)`,
		ev.ID, ev.UserID, ev.Kind, ev.ProjectSlug, query, string(itemsJSON), hit,
		ev.CreatedAt.Format(time.RFC3339),
	)
	if err != nil {
		return fmt.Errorf("usage.RetrievalStore.Insert: %w", err)
	}
	return nil
}

// RetrievalKindStat is per-kind aggregate counts.
type RetrievalKindStat struct {
	Kind  string `json:"kind"`
	Total int    `json:"total"`
	Hits  int    `json:"hits"`
}

// RetrievalSummary aggregates retrieval telemetry since a cutoff.
type RetrievalSummary struct {
	Since time.Time           `json:"since"`
	Total int                 `json:"total"`
	Kinds []RetrievalKindStat `json:"kinds"`
	// ReadAfterInjectRate is the fraction of key-bearing injection events
	// (prompt_context / briefing) followed within 15 minutes by a memory_read
	// of an overlapping item key. Injection events with no item keys (e.g.
	// briefings, which record a serve flag but not per-key data) are excluded.
	ReadAfterInjectRate float64 `json:"read_after_inject_rate"`
	InjectionEvents     int     `json:"injection_events"`
	ReadFollowups       int     `json:"read_followups"`
}

// Summary aggregates events since the cutoff, including the read-after-inject
// rate computed via an in-Go time-window join (correctness over cleverness).
func (s *RetrievalStore) Summary(ctx context.Context, db DBTX, since time.Time) (*RetrievalSummary, error) {
	rows, err := db.QueryContext(ctx,
		`SELECT kind, items, hit, created_at FROM retrieval_events
		 WHERE created_at >= ? ORDER BY created_at ASC`,
		since.Format(time.RFC3339),
	)
	if err != nil {
		return nil, fmt.Errorf("usage.RetrievalStore.Summary: %w", err)
	}
	defer rows.Close()

	type event struct {
		kind  string
		items []string
		at    time.Time
	}
	var all []event
	kindStats := map[string]*RetrievalKindStat{}
	total := 0
	for rows.Next() {
		var kind, itemsJSON, createdAt string
		var hitInt int
		if err := rows.Scan(&kind, &itemsJSON, &hitInt, &createdAt); err != nil {
			return nil, fmt.Errorf("usage.RetrievalStore.Summary: scan: %w", err)
		}
		var items []string
		_ = json.Unmarshal([]byte(itemsJSON), &items)
		at, _ := time.Parse(time.RFC3339, createdAt)
		all = append(all, event{kind: kind, items: items, at: at})

		st := kindStats[kind]
		if st == nil {
			st = &RetrievalKindStat{Kind: kind}
			kindStats[kind] = st
		}
		st.Total++
		if hitInt != 0 {
			st.Hits++
		}
		total++
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("usage.RetrievalStore.Summary: rows: %w", err)
	}

	// Read-after-inject: for each key-bearing injection, look for a later
	// memory_read (within the window) sharing an item key.
	var reads []event
	for _, e := range all {
		if e.kind == RetrievalKindMemoryRead {
			reads = append(reads, e)
		}
	}
	injections, followed := 0, 0
	for _, e := range all {
		if (e.kind != RetrievalKindBriefing && e.kind != RetrievalKindPromptContext) || len(e.items) == 0 {
			continue
		}
		injections++
		injSet := make(map[string]struct{}, len(e.items))
		for _, it := range e.items {
			injSet[it] = struct{}{}
		}
		windowEnd := e.at.Add(readAfterInjectWindow)
		for _, rd := range reads {
			if !rd.at.After(e.at) || rd.at.After(windowEnd) {
				continue
			}
			matched := false
			for _, it := range rd.items {
				if _, ok := injSet[it]; ok {
					matched = true
					break
				}
			}
			if matched {
				followed++
				break
			}
		}
	}

	kinds := make([]RetrievalKindStat, 0, len(kindStats))
	for _, st := range kindStats {
		kinds = append(kinds, *st)
	}
	sort.Slice(kinds, func(i, j int) bool { return kinds[i].Kind < kinds[j].Kind })

	rate := 0.0
	if injections > 0 {
		rate = float64(followed) / float64(injections)
	}

	return &RetrievalSummary{
		Since:               since,
		Total:               total,
		Kinds:               kinds,
		ReadAfterInjectRate: rate,
		InjectionEvents:     injections,
		ReadFollowups:       followed,
	}, nil
}

// RetrievalRecorder records retrieval events fire-and-forget. It mirrors the
// Tracker pattern: the store's Insert takes a DBTX and neither the hook handler
// nor the MCP server holds a DB handle, so the recorder opens the user DB
// itself. Record never blocks the caller or returns an error.
type RetrievalRecorder struct {
	store     *RetrievalStore
	dbManager userdb.Manager
	logger    *slog.Logger
}

// NewRetrievalRecorder creates a RetrievalRecorder.
func NewRetrievalRecorder(store *RetrievalStore, dbManager userdb.Manager, logger *slog.Logger) *RetrievalRecorder {
	if logger == nil {
		logger = slog.Default()
	}
	return &RetrievalRecorder{store: store, dbManager: dbManager, logger: logger}
}

// Record persists ev in a detached goroutine (context.WithoutCancel + 2s
// timeout) so it survives the request returning. Failures are logged, never
// propagated.
func (rr *RetrievalRecorder) Record(ctx context.Context, ev *RetrievalEvent) {
	if ev == nil {
		return
	}
	if ev.ID == "" {
		id, err := ulid.New(ulid.Now(), rand.Reader)
		if err != nil {
			rr.logger.Warn("usage.RetrievalRecorder: generate id", "error", err)
			return
		}
		ev.ID = id.String()
	}
	if ev.CreatedAt.IsZero() {
		ev.CreatedAt = time.Now().UTC()
	}
	if ev.UserID == "" {
		ev.UserID = reqctx.UserIDFromContext(ctx)
	}
	if ev.UserID == "" {
		ev.UserID = userdb.DefaultUserID
	}

	detached := context.WithoutCancel(ctx)
	go func() {
		cctx, cancel := context.WithTimeout(detached, 2*time.Second)
		defer cancel()
		db, err := rr.dbManager.Open(cctx, ev.UserID)
		if err != nil {
			rr.logger.Warn("usage.RetrievalRecorder: open db", "error", err)
			return
		}
		if err := rr.store.Insert(cctx, db, ev); err != nil {
			rr.logger.Warn("usage.RetrievalRecorder: insert", "error", err)
		}
	}()
}
