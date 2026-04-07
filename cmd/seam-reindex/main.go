// seam-reindex re-embeds every note for the default user against the
// embedding provider/model configured in seam-server.yaml.
//
// Why this exists: Chroma collections have implicit dimensions (set on
// first insert). Switching the embedding model -- whether between Ollama
// model variants or between Ollama and OpenAI -- means a new collection
// name (see ai.CollectionName), so the new collection starts empty and
// search returns nothing until the notes are re-embedded. This binary
// drives that one-shot reindex synchronously, bypassing ai.Queue. It
// is safe to run while seamd is up because Embedder.EmbedNote upserts.
//
// Usage:
//
//	./bin/seam-reindex                      # uses ./seam-server.yaml
//	./bin/seam-reindex -config /path/to.yml
package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/config"
	"github.com/katata/seam/internal/userdb"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seam-reindex: %v\n", err)
		os.Exit(1)
	}
}

func run() error {
	cfgPath := flag.String("config", "seam-server.yaml", "config file path")
	flag.Parse()

	cfg, err := config.Load(*cfgPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))

	if cfg.ChromaDBURL == "" {
		return fmt.Errorf("chromadb_url is empty; nothing to reindex (set chromadb_url in %s)", *cfgPath)
	}

	// Open seam.db the same way seamd does, then wrap it in a userdb manager
	// so the embedder can read notes.
	seamDBPath := filepath.Join(cfg.DataDir, "seam.db")
	seamDB, err := auth.OpenDB(seamDBPath)
	if err != nil {
		return fmt.Errorf("open seam.db at %s: %w", seamDBPath, err)
	}
	defer func() { _ = seamDB.Close() }()

	userDBMgr := userdb.NewSQLManagerWithDB(seamDB, cfg.DataDir, logger)
	defer func() { _ = userDBMgr.CloseAll() }()

	// Build the embedding client using the same switch as seamd. Whichever
	// provider is configured in YAML wins -- if you ran seamd with ollama
	// and then switched models in the YAML, this binary picks the new model
	// and writes to the new Chroma collection.
	var ollamaClient *ai.OllamaClient
	if cfg.OllamaBaseURL != "" {
		ollamaClient = ai.NewOllamaClient(
			cfg.OllamaBaseURL,
			cfg.AI.EmbeddingTimeout.Duration,
			cfg.AI.ChatTimeout.Duration,
		)
	}

	var embeddingClient ai.EmbeddingGenerator
	switch cfg.Embeddings.Provider {
	case "openai":
		embeddingClient = ai.NewOpenAIEmbedder(
			cfg.Embeddings.OpenAI.APIKey,
			cfg.Embeddings.OpenAI.BaseURL,
			cfg.Embeddings.OpenAI.Dimensions,
			cfg.AI.EmbeddingTimeout.Duration,
		)
		logger.Info("embedding provider: OpenAI",
			"model", cfg.Models.Embeddings,
			"dimensions", cfg.Embeddings.OpenAI.Dimensions)
	default: // "ollama"
		if ollamaClient == nil {
			return fmt.Errorf("embeddings.provider=ollama but ollama_base_url is empty")
		}
		embeddingClient = ollamaClient
		logger.Info("embedding provider: Ollama (local)", "model", cfg.Models.Embeddings)
	}

	chromaClient := ai.NewChromaClient(cfg.ChromaDBURL)

	ctx := context.Background()

	// Probe Chroma so the operator gets an immediate failure rather than
	// silent enqueue-and-fail behaviour.
	if err := chromaClient.Heartbeat(ctx); err != nil {
		return fmt.Errorf("chromadb unreachable at %s: %w (is `make chroma-up` running?)", cfg.ChromaDBURL, err)
	}

	embedder := ai.NewEmbedder(embeddingClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)

	collection := ai.CollectionName(userdb.DefaultUserID, cfg.Models.Embeddings)
	logger.Info("seam-reindex: target collection", "name", collection)

	db, err := userDBMgr.Open(ctx, userdb.DefaultUserID)
	if err != nil {
		return fmt.Errorf("open user db: %w", err)
	}

	rows, err := db.QueryContext(ctx, `SELECT id, title, body FROM notes ORDER BY created_at ASC`)
	if err != nil {
		return fmt.Errorf("query notes: %w", err)
	}

	type note struct {
		id, title, body string
	}
	var notes []note
	for rows.Next() {
		var n note
		if err := rows.Scan(&n.id, &n.title, &n.body); err != nil {
			rows.Close()
			return fmt.Errorf("scan note: %w", err)
		}
		notes = append(notes, n)
	}
	if err := rows.Err(); err != nil {
		rows.Close()
		return fmt.Errorf("iterate notes: %w", err)
	}
	rows.Close()

	logger.Info("seam-reindex: starting",
		"count", len(notes),
		"provider", cfg.Embeddings.Provider,
		"model", cfg.Models.Embeddings,
		"collection", collection)

	if len(notes) == 0 {
		fmt.Fprintln(os.Stderr, "seam-reindex: no notes to reindex")
		return nil
	}

	succ, fail := 0, 0
	for i, n := range notes {
		if err := embedder.EmbedNote(ctx, userdb.DefaultUserID, n.id, n.title, n.body); err != nil {
			logger.Warn("seam-reindex: embed failed", "note_id", n.id, "error", err)
			fail++
			continue
		}
		succ++
		if (i+1)%25 == 0 || i+1 == len(notes) {
			fmt.Fprintf(os.Stderr, "  %d/%d notes embedded\n", i+1, len(notes))
		}
	}

	fmt.Fprintf(os.Stderr, "seam-reindex: done. %d ok, %d failed\n", succ, fail)
	if fail > 0 {
		os.Exit(2)
	}
	return nil
}
