package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/assistant"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/briefing"
	"github.com/katata/seam/internal/capture"
	"github.com/katata/seam/internal/chat"
	"github.com/katata/seam/internal/config"
	"github.com/katata/seam/internal/graph"
	seamMCP "github.com/katata/seam/internal/mcp"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/scheduler"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/server"
	"github.com/katata/seam/internal/settings"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/template"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/validate"
	"github.com/katata/seam/internal/watcher"
	"github.com/katata/seam/internal/webhook"
	"github.com/katata/seam/internal/ws"
)

// version is set at build time via -ldflags "-X main.version=x.y.z".
// Defaults to "dev" for local builds.
var version = "dev"

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seamd: %v\n", err)
		os.Exit(1)
	}
}

// defaultDailyBriefingID is the stable schedule ID assigned to the
// auto-provisioned daily briefing. Using a fixed ID (rather than the
// schedule's name) lets the user rename the schedule via the API
// without re-provisioning a duplicate on the next server restart.
const defaultDailyBriefingID = "default_daily_briefing"

// provisionDailyBriefing creates the default daily briefing schedule on
// startup if it has not been provisioned before. Idempotency is keyed
// on a fixed schedule ID so subsequent restarts skip provisioning even
// if the user has renamed the schedule.
func provisionDailyBriefing(ctx context.Context, svc *scheduler.Service, cfg config.DailyBriefingConfig, logger *slog.Logger) error {
	const briefingName = "Daily Briefing"

	existing, err := svc.Get(ctx, userdb.DefaultUserID, defaultDailyBriefingID)
	if err == nil {
		logger.Debug("daily briefing schedule already provisioned",
			"id", existing.ID, "name", existing.Name, "next_run", existing.NextRunAt)
		return nil
	}
	if !errors.Is(err, scheduler.ErrNotFound) {
		return fmt.Errorf("lookup default schedule: %w", err)
	}

	configJSON, err := json.Marshal(briefing.ActionConfig{
		ProjectSlug:   cfg.ProjectSlug,
		LookbackHours: cfg.LookbackHours,
	})
	if err != nil {
		return fmt.Errorf("marshal action config: %w", err)
	}

	enabled := true
	created, err := svc.Create(ctx, userdb.DefaultUserID, scheduler.CreateReq{
		ID:           defaultDailyBriefingID,
		Name:         briefingName,
		CronExpr:     cfg.CronExpr,
		ActionType:   scheduler.ActionBriefing,
		ActionConfig: configJSON,
		Enabled:      &enabled,
	})
	if err != nil {
		return fmt.Errorf("create schedule: %w", err)
	}
	logger.Info("provisioned daily briefing schedule",
		"id", created.ID, "cron", created.CronExpr, "next_run", created.NextRunAt)
	return nil
}

// noteBodyAdapter adapts the note service to the ai.NoteBodyLoader and
// ai.NoteBodyUpdater interfaces, respecting the layering rule.
type noteBodyAdapter struct {
	noteSvc *note.Service
}

func (a *noteBodyAdapter) LoadNoteBody(ctx context.Context, userID, noteID string) (string, error) {
	n, err := a.noteSvc.Get(ctx, userID, noteID)
	if err != nil {
		return "", fmt.Errorf("noteBodyAdapter.LoadNoteBody: %w", err)
	}
	return n.Body, nil
}

func (a *noteBodyAdapter) UpdateNoteBody(ctx context.Context, userID, noteID, body string) error {
	_, err := a.noteSvc.Update(ctx, userID, noteID, note.UpdateNoteReq{Body: &body})
	if err != nil {
		return fmt.Errorf("noteBodyAdapter.UpdateNoteBody: %w", err)
	}
	return nil
}

func run() error {
	configPath := flag.String("config", "seam-server.yaml", "path to configuration file")
	flag.Parse()

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Set up structured logging with configurable level.
	var logLevel slog.Level
	switch strings.ToLower(cfg.LogLevel) {
	case "debug":
		logLevel = slog.LevelDebug
	case "warn":
		logLevel = slog.LevelWarn
	case "error":
		logLevel = slog.LevelError
	default:
		logLevel = slog.LevelInfo
	}
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: logLevel,
	}))
	slog.SetDefault(logger)

	// Create data directory if it does not exist.
	if err := os.MkdirAll(cfg.DataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir: %w", err)
	}

	// Open seam.db -- single database for everything (owner, notes, projects, etc.).
	seamDBPath := filepath.Join(cfg.DataDir, "seam.db")
	seamDB, err := auth.OpenDB(seamDBPath)
	if err != nil {
		return fmt.Errorf("open seam db: %w", err)
	}

	// Create database manager backed by the same DB handle.
	userDBMgr := userdb.NewSQLManagerWithDB(seamDB, cfg.DataDir, logger)
	// NOTE: userDBMgr.CloseAll is deferred here but watcher.Close and
	// aiQueue shutdown are deferred AFTER this (below), so in LIFO order
	// the watcher and AI queue stop before the DB is closed.
	defer func() {
		if err := userDBMgr.CloseAll(); err != nil {
			logger.Warn("userDBMgr.CloseAll", "error", err)
		}
	}()

	// Set up context with signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start manager run loop.
	go userDBMgr.Run(ctx)

	// Create auth components (same DB handle).
	authStore := auth.NewSQLStore(seamDB)
	jwtMgr := auth.NewJWTManager(cfg.JWTSecret, cfg.Auth.AccessTokenTTL.Duration)
	authSvc := auth.NewService(
		authStore, jwtMgr, userDBMgr,
		cfg.Auth.RefreshTokenTTL.Duration,
		cfg.Auth.BcryptCost, logger,
	)
	authHandler := auth.NewHandler(authSvc, logger)

	// Start periodic cleanup of expired refresh tokens.
	go func() {
		ticker := time.NewTicker(1 * time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				return
			case <-ticker.C:
				if err := authStore.DeleteExpiredTokens(ctx); err != nil {
					logger.Warn("expired token cleanup failed", "error", err)
				} else {
					logger.Debug("expired refresh tokens cleaned up")
				}
			}
		}
	}()

	// Create project components.
	projectStore := project.NewStore()
	projectSvc := project.NewService(projectStore, userDBMgr, logger)
	projectHandler := project.NewHandler(projectSvc, logger)

	// Create note components.
	noteStore := note.NewSQLStore()
	versionStore := note.NewVersionStore()
	noteSvc := note.NewService(noteStore, versionStore, projectStore, userDBMgr, nil, logger) // suppressor set below
	noteHandler := note.NewHandler(noteSvc, logger)

	// Create task tracking components.
	taskStore := task.NewStore()
	taskSvc := task.NewService(taskStore, userDBMgr, logger)
	taskSvc.SetNoteService(noteSvc)
	taskHandler := task.NewHandler(taskSvc, logger)

	// C-19: Wire frontmatter updater so project cascade-to-inbox clears
	// the project field from YAML frontmatter on disk.
	projectSvc.SetFrontmatterUpdater(func(notesDir, filePath string) error {
		if err := validate.PathWithinDir(filePath, notesDir); err != nil {
			return fmt.Errorf("frontmatter updater: %w", err)
		}
		absPath := filepath.Join(notesDir, filePath)
		data, err := os.ReadFile(absPath)
		if err != nil {
			return err
		}
		fm, body, err := note.ParseFrontmatter(string(data))
		if err != nil {
			return err
		}
		fm.Project = ""
		content, err := note.SerializeFrontmatter(fm, body)
		if err != nil {
			return err
		}
		// Preserve original file permissions and write atomically.
		info, statErr := os.Stat(absPath)
		perm := os.FileMode(0o644)
		if statErr == nil {
			perm = info.Mode().Perm()
		}
		return note.AtomicWriteFile(absPath, []byte(content), perm)
	})

	// Create search components.
	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)
	searchHandler := search.NewHandler(searchSvc, logger)

	// Create capture components.
	capture.Version = version
	urlFetcher := capture.NewURLFetcher()
	var voiceTranscriber *capture.VoiceTranscriber
	if cfg.Whisper.ModelPath != "" {
		voiceTranscriber = capture.NewVoiceTranscriber(cfg.Whisper.BinaryPath, cfg.Whisper.ModelPath)
	}
	captureSvc := capture.NewService(noteSvc, urlFetcher, voiceTranscriber, logger)
	captureHandler := capture.NewHandler(captureSvc, logger)

	// Create template components.
	templateSvc := template.NewService(cfg.DataDir, userDBMgr, logger)
	if err := templateSvc.EnsureDefaults(); err != nil {
		logger.Warn("failed to create default templates", "error", err)
	}
	templateHandler := template.NewHandler(templateSvc, logger)

	// Wire template service into note handler for single-request template-based creation.
	noteHandler.SetTemplateApplier(templateSvc)

	// Create graph components.
	graphSvc := graph.NewService(userDBMgr, logger)
	graphHandler := graph.NewHandler(graphSvc, logger)

	// Create settings components.
	settingsStore := settings.NewStore()
	settingsSvc := settings.NewService(settingsStore, userDBMgr, logger)
	settingsHandler := settings.NewHandler(settingsSvc, logger)

	// Create chat history components.
	chatHistoryStore := chat.NewStore()
	chatHistorySvc := chat.NewService(chatHistoryStore, userDBMgr, logger)
	chatHistoryHandler := chat.NewHandler(chatHistorySvc, logger)

	// Create AI components.
	// Ollama is created for embeddings when configured (local vector generation).
	var ollamaClient *ai.OllamaClient
	if cfg.OllamaBaseURL != "" {
		ollamaClient = ai.NewOllamaClient(
			cfg.OllamaBaseURL,
			cfg.AI.EmbeddingTimeout.Duration,
			cfg.AI.ChatTimeout.Duration,
		)
	}

	// Select the ChatCompleter based on the configured LLM provider.
	var chatCompleter ai.ChatCompleter
	switch cfg.LLM.Provider {
	case "openai":
		chatCompleter = ai.NewOpenAIClient(
			cfg.LLM.OpenAI.APIKey,
			cfg.LLM.OpenAI.BaseURL,
			cfg.AI.ChatTimeout.Duration,
		)
		logger.Info("LLM provider: OpenAI", "base_url", cfg.LLM.OpenAI.BaseURL)
	case "anthropic":
		chatCompleter = ai.NewAnthropicClient(
			cfg.LLM.Anthropic.APIKey,
			cfg.AI.ChatTimeout.Duration,
			cfg.LLM.Anthropic.MaxTokens,
		)
		logger.Info("LLM provider: Anthropic")
	default: // "ollama"
		if ollamaClient != nil {
			chatCompleter = ollamaClient
		}
		logger.Info("LLM provider: Ollama (local)")
	}

	// Select the EmbeddingGenerator based on embeddings.provider.
	// This is independent of the chat provider above: a user with Anthropic
	// chat may legitimately want OpenAI or Ollama embeddings (Anthropic ships
	// no embedding model). Defaults to Ollama.
	var embeddingClient ai.EmbeddingGenerator
	switch cfg.Embeddings.Provider {
	case "openai":
		embeddingClient = ai.NewOpenAIEmbedder(
			cfg.Embeddings.OpenAI.APIKey,
			cfg.Embeddings.OpenAI.BaseURL,
			cfg.Embeddings.OpenAI.Dimensions,
			cfg.AI.EmbeddingTimeout.Duration,
		)
		logger.Info("Embedding provider: OpenAI",
			"base_url", cfg.Embeddings.OpenAI.BaseURL,
			"model", cfg.Models.Embeddings,
			"dimensions", cfg.Embeddings.OpenAI.Dimensions)
	default: // "ollama"
		if ollamaClient != nil {
			embeddingClient = ollamaClient
		}
		logger.Info("Embedding provider: Ollama (local)", "model", cfg.Models.Embeddings)
	}

	var aiHandler *ai.Handler
	var aiQueue *ai.Queue
	var chatSvc *ai.ChatService
	var synthSvc *ai.Synthesizer

	// Create WebSocket hub.
	hub := ws.NewHub(logger)

	// Wire up AI components (only if ChromaDB is configured).
	aiTaskStore := ai.NewTaskStore()
	aiQueue = ai.NewQueue(aiTaskStore, userDBMgr, hub, cfg.AI.QueueWorkers, logger)

	var embedder *ai.Embedder
	var chromaClient *ai.ChromaClient

	if cfg.ChromaDBURL != "" && embeddingClient != nil {
		chromaClient = ai.NewChromaClient(cfg.ChromaDBURL)

		// Probe ChromaDB at startup so the operator gets an immediate, loud
		// signal if the container is not running. We do not block startup --
		// the AI task queue will retry naturally once Chroma comes up.
		probeCtx, probeCancel := context.WithTimeout(ctx, 2*time.Second)
		if probeErr := chromaClient.Heartbeat(probeCtx); probeErr != nil {
			logger.Warn("ChromaDB unreachable at startup; semantic search and embeddings will queue until it is available",
				"chromadb_url", cfg.ChromaDBURL,
				"error", probeErr,
				"hint", "run `make chroma-up`, or install the supervisor service via `make install-service`")
		}
		probeCancel()

		embedder = ai.NewEmbedder(embeddingClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)
		chatSvc = ai.NewChatService(embeddingClient, chatCompleter, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Chat, logger)
		synthSvc = ai.NewSynthesizer(chatCompleter, userDBMgr, cfg.Models.Chat, logger)
		linker := ai.NewAutoLinker(embeddingClient, chatCompleter, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Background, hub, logger)

		// Register task handlers.
		aiQueue.RegisterHandler(ai.TaskTypeEmbed, embedder.HandleEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeDeleteEmbed, embedder.HandleDeleteEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeChat, chatSvc.HandleChatTask)
		aiQueue.RegisterHandler(ai.TaskTypeSynthesize, synthSvc.HandleSynthesizeTask)
		aiQueue.RegisterHandler(ai.TaskTypeAutolink, linker.HandleAutolinkTask)

		// Create AI writer (uses chat model for writing assist).
		aiWriter := ai.NewWriter(chatCompleter, userDBMgr, cfg.Models.Chat, logger)
		bodyAdapter := &noteBodyAdapter{noteSvc: noteSvc}
		aiWriter.SetNoteBodyLoader(bodyAdapter)
		aiWriter.SetNoteBodyUpdater(bodyAdapter)
		aiQueue.RegisterHandler(ai.TaskTypeAssist, aiWriter.HandleAssistTask)
		aiQueue.RegisterHandler(ai.TaskTypeSummarizeTranscript, aiWriter.HandleSummarizeTranscriptTask)

		// Wire background summarization for voice captures.
		captureSvc.SetSummarizeFunc(func(ctx context.Context, userID, noteID string) {
			payload, marshalErr := json.Marshal(ai.SummarizeTranscriptPayload{NoteID: noteID})
			if marshalErr != nil {
				logger.Warn("failed to marshal summarize payload", "error", marshalErr)
				return
			}
			if err := aiQueue.Enqueue(ctx, &ai.Task{
				UserID:   userID,
				Type:     ai.TaskTypeSummarizeTranscript,
				Priority: ai.PriorityBackground,
				Payload:  payload,
			}); err != nil {
				logger.Warn("failed to enqueue transcript summarization", "note_id", noteID, "error", err)
			}
		})

		// Enable semantic search using the configured embedding provider.
		semanticSearcher := search.NewSemanticSearcher(embeddingClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)
		searchSvc.SetSemanticSearcher(semanticSearcher)

		// Create AI suggester for tag/project suggestions.
		suggester := ai.NewSuggester(chatCompleter, cfg.Models.Chat, logger)

		aiHandler = ai.NewHandler(aiQueue, chatSvc, synthSvc, linker, embedder, aiWriter, suggester, userDBMgr, logger)
		logger.Info("AI features enabled", "ollama_url", cfg.OllamaBaseURL, "chromadb_url", cfg.ChromaDBURL)
	} else if chatCompleter != nil {
		// Without ChromaDB (or an embedding client), writer and suggester
		// can still work with any chat provider.
		aiWriter := ai.NewWriter(chatCompleter, userDBMgr, cfg.Models.Chat, logger)
		bodyAdapter := &noteBodyAdapter{noteSvc: noteSvc}
		aiWriter.SetNoteBodyLoader(bodyAdapter)
		aiWriter.SetNoteBodyUpdater(bodyAdapter)
		suggester := ai.NewSuggester(chatCompleter, cfg.Models.Chat, logger)
		aiHandler = ai.NewHandler(nil, nil, nil, nil, nil, aiWriter, suggester, userDBMgr, logger)
		if cfg.ChromaDBURL != "" && embeddingClient == nil {
			logger.Info("AI features: embedding client not configured (embeddings.provider=ollama with no ollama_base_url); embeddings/RAG disabled, writing assist and suggestions available")
		} else {
			logger.Info("AI features: ChromaDB not configured; only writing assist and suggestions available")
		}
	} else {
		aiHandler = ai.NewHandler(nil, nil, nil, nil, nil, nil, nil, userDBMgr, logger)
		logger.Info("AI features: no LLM provider configured; AI features disabled")
	}

	// Forward-declare webhookSvc so the file watcher closure can capture it.
	var webhookSvc *webhook.Service

	// Create file watcher with note.Reindex as the event handler.
	fileHandler := func(fctx context.Context, uid, filePath string) error {
		// Before reindex, check if the note already exists in the DB to
		// determine the change type (created vs modified vs deleted).
		userDB, dbErr := userDBMgr.Open(fctx, uid)
		var existedBefore bool
		var existingNoteID string
		if dbErr == nil {
			existing, getErr := noteStore.GetByFilePath(fctx, userDB, filePath)
			if getErr == nil {
				existedBefore = true
				existingNoteID = existing.ID
			}
		}

		reindexErr := noteSvc.Reindex(fctx, uid, filePath)
		if reindexErr != nil {
			logger.Error("reindex failed", "user_id", uid, "file_path", filePath, "error", reindexErr)
			return reindexErr
		}

		// Determine change type and resolve note ID.
		var noteID string
		var changeType string

		// Check if file exists on disk after reindex.
		notesDir := userDBMgr.UserNotesDir(uid)
		absPath := filepath.Join(notesDir, filePath)
		_, statErr := os.Stat(absPath)
		fileExists := statErr == nil

		if !fileExists && existedBefore {
			changeType = "deleted"
			noteID = existingNoteID
		} else if fileExists && !existedBefore {
			changeType = "created"
			// Look up the newly created note.
			if dbErr == nil {
				n, getErr := noteStore.GetByFilePath(fctx, userDB, filePath)
				if getErr == nil {
					noteID = n.ID
				}
			}
		} else {
			changeType = "modified"
			if dbErr == nil {
				n, getErr := noteStore.GetByFilePath(fctx, userDB, filePath)
				if getErr == nil {
					noteID = n.ID
				}
			}
		}

		if noteID == "" {
			noteID = filePath // fallback
		}

		// Push note.changed event to the user's WebSocket connections.
		payload, marshalErr := json.Marshal(ws.NoteChangedPayload{
			NoteID:     noteID,
			ChangeType: changeType,
		})
		if marshalErr != nil {
			logger.Warn("failed to marshal note changed payload", "error", marshalErr)
			return nil
		}
		if err := hub.Send(uid, ws.Message{Type: ws.MsgTypeNoteChanged, Payload: payload}); err != nil {
			logger.Debug("hub.Send note.changed", "user_id", uid, "error", err)
		}

		// Enqueue embedding tasks if AI is enabled.
		// C-32: Log enqueue errors instead of silently discarding them.
		if embedder != nil && aiQueue != nil {
			switch changeType {
			case "created", "modified":
				embedPayload, _ := json.Marshal(ai.EmbedPayload{NoteID: noteID}) //nolint:errcheck // simple struct
				if err := aiQueue.Enqueue(fctx, &ai.Task{
					UserID:   uid,
					Type:     ai.TaskTypeEmbed,
					Priority: ai.PriorityBackground,
					Payload:  embedPayload,
				}); err != nil {
					logger.Warn("failed to enqueue embed task", "note_id", noteID, "error", err)
				}
			case "deleted":
				deletePayload, _ := json.Marshal(ai.DeleteEmbedPayload{NoteID: noteID}) //nolint:errcheck // simple struct
				if err := aiQueue.Enqueue(fctx, &ai.Task{
					UserID:   uid,
					Type:     ai.TaskTypeDeleteEmbed,
					Priority: ai.PriorityBackground,
					Payload:  deletePayload,
				}); err != nil {
					logger.Warn("failed to enqueue delete embed task", "note_id", noteID, "error", err)
				}
			}
		}

		// Sync tasks (checkbox items) for created/modified notes.
		if changeType == "created" || changeType == "modified" {
			if noteID != "" && noteID != filePath {
				if dbErr == nil {
					n, getErr := noteStore.Get(fctx, userDB, noteID)
					if getErr == nil {
						if syncErr := taskSvc.SyncNote(fctx, uid, noteID, n.Body); syncErr != nil {
							logger.Warn("task sync failed", "note_id", noteID, "error", syncErr)
						}
					}
				}
			}
		}

		// Dispatch webhook events.
		if webhookSvc != nil {
			eventPayload := map[string]interface{}{
				"note_id":     noteID,
				"change_type": changeType,
			}
			webhookSvc.Dispatch(context.Background(), uid, "note."+changeType, eventPayload)
		}

		return nil
	}

	w, err := watcher.NewWatcher(fileHandler, cfg.Watcher.DebounceInterval.Duration, logger)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
	// Deferred after userDBMgr.CloseAll, so in LIFO order the watcher
	// closes before user databases are closed.
	defer w.Close()

	// Wire the watcher as the note service's write suppressor.
	noteSvc.SetSuppressor(w)

	// Load pending AI tasks from a previous run BEFORE reconciliation,
	// so that reconciliation-enqueued tasks are not double-loaded.
	if aiQueue != nil {
		if err := aiQueue.LoadPending(ctx); err != nil {
			logger.Warn("failed to load pending AI tasks", "error", err)
		}
	}

	// Run startup reconciliation for all existing users.
	users, err := userDBMgr.ListUsers(ctx)
	if err != nil {
		logger.Warn("failed to list users for reconciliation", "error", err)
	} else {
		for _, uid := range users {
			notesDir := userDBMgr.UserNotesDir(uid)
			// Start watching before reconciliation to avoid missing changes.
			if watchErr := w.Watch(uid, notesDir); watchErr != nil {
				logger.Warn("failed to watch user notes dir", "user_id", uid, "error", watchErr)
				continue
			}
			userDB, dbErr := userDBMgr.Open(ctx, uid)
			if dbErr != nil {
				logger.Warn("failed to open user db for reconciliation", "user_id", uid, "error", dbErr)
				continue
			}
			if recErr := watcher.Reconcile(ctx, uid, notesDir, fileHandler, userDB); recErr != nil {
				logger.Warn("reconciliation failed", "user_id", uid, "error", recErr)
			}
		}
		logger.Info("startup reconciliation complete", "users_scanned", len(users))
	}

	// Start watcher event loop.
	go func() {
		if err := w.Run(ctx); err != nil {
			logger.Error("watcher stopped", "error", err)
		}
	}()

	// Start AI task queue workers.
	// aiQueueDone is closed when the queue workers finish, so the shutdown
	// sequence can wait for in-flight tasks before closing databases.
	aiQueueDone := make(chan struct{})
	if aiQueue != nil {
		go func() {
			defer close(aiQueueDone)
			if err := aiQueue.Run(ctx); err != nil {
				logger.Error("AI queue stopped", "error", err)
			}
		}()
	} else {
		close(aiQueueDone)
	}

	// Build WebSocket message handler for chat.ask and synthesize.stream.
	// Reuses the chatSvc and synthSvc created in the ChromaDB block above.
	var wsHandler ws.MessageHandler
	if cfg.ChromaDBURL != "" && chatSvc != nil {
		wsHandler = func(fctx context.Context, h *ws.Hub, conn *websocket.Conn, uid string, msg ws.Message) {
			switch msg.Type {
			case ws.MsgTypeChatAsk:
				var payload struct {
					Query   string           `json:"query"`
					History []ai.ChatMessage `json:"history,omitempty"`
					Summary string           `json:"summary,omitempty"`
				}
				if err := json.Unmarshal(msg.Payload, &payload); err != nil {
					return
				}

				// Validate history message roles before streaming.
				for _, m := range payload.History {
					if m.Role != "user" && m.Role != "assistant" {
						errPayload, _ := json.Marshal(map[string]string{"error": "invalid message role in history"})
						data, _ := json.Marshal(ws.Message{
							Type:    ws.MsgTypeChatDone,
							Payload: errPayload,
						})
						writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						_ = conn.Write(writeCtx, websocket.MessageText, data)
						cancel()
						return
					}
				}

				tokenCh, citations, errCh := chatSvc.AskStream(fctx, uid, payload.Query, payload.History, payload.Summary)

				// Stream tokens to client.
				go func() {
					writeFailed := false
					for token := range tokenCh {
						if writeFailed {
							continue // drain channel
						}
						tokenPayload, _ := json.Marshal(map[string]string{"token": token})
						data, _ := json.Marshal(ws.Message{
							Type:    ws.MsgTypeChatStream,
							Payload: tokenPayload,
						})
						writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						err := conn.Write(writeCtx, websocket.MessageText, data)
						cancel()
						if err != nil {
							logger.Debug("chat stream write failed, stopping", "error", err)
							writeFailed = true
						}
					}

					// Check for errors.
					for err := range errCh {
						if err != nil {
							logger.Error("chat stream error", "error", err)
						}
					}

					if writeFailed {
						return
					}

					// Send done message with citations.
					donePayload, _ := json.Marshal(map[string]interface{}{"citations": citations})
					data, _ := json.Marshal(ws.Message{
						Type:    ws.MsgTypeChatDone,
						Payload: donePayload,
					})
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = conn.Write(writeCtx, websocket.MessageText, data)
					cancel()
				}()

			case "synthesize.stream":
				var payload ai.SynthesizePayload
				if err := json.Unmarshal(msg.Payload, &payload); err != nil {
					return
				}
				if payload.Scope == "" || payload.Prompt == "" {
					return
				}

				tokenCh, errCh := synthSvc.SynthesizeStream(fctx, uid, payload)

				go func() {
					writeFailed := false
					for token := range tokenCh {
						if writeFailed {
							continue // drain channel
						}
						tokenPayload, _ := json.Marshal(map[string]string{"token": token})
						data, _ := json.Marshal(ws.Message{
							Type:    "synthesize.token",
							Payload: tokenPayload,
						})
						writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						err := conn.Write(writeCtx, websocket.MessageText, data)
						cancel()
						if err != nil {
							logger.Debug("synthesize stream write failed, stopping", "error", err)
							writeFailed = true
						}
					}

					for err := range errCh {
						if err != nil {
							logger.Error("synthesize stream error", "error", err)
						}
					}

					if writeFailed {
						return
					}

					donePayload, _ := json.Marshal(map[string]string{})
					data, _ := json.Marshal(ws.Message{
						Type:    "synthesize.done",
						Payload: donePayload,
					})
					writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
					_ = conn.Write(writeCtx, websocket.MessageText, data)
					cancel()
				}()

			default:
				logger.Debug("ws: unhandled message type", "type", msg.Type)
			}
		}
	}

	// Create review queue components (knowledge gardening).
	reviewSvc := review.NewService(userDBMgr, graphSvc, logger)
	reviewHandler := review.NewHandler(reviewSvc, logger)

	// Create webhook components.
	webhookStore := webhook.NewStore()
	webhookSvc = webhook.NewService(webhookStore, userDBMgr, logger)
	webhookHandler := webhook.NewHandler(webhookSvc, logger)

	// Create agent memory / MCP components.
	agentStore := agent.NewSQLStore()
	agentWSNotifier := agent.NewHubWSNotifier(hub, logger)
	agentSvc := agent.NewService(agent.ServiceConfig{
		Store:          agentStore,
		NoteService:    noteSvc,
		ProjectService: projectSvc,
		SearchService:  searchSvc,
		AIQueue:        aiQueue,
		WSNotifier:     agentWSNotifier,
		UserDBManager:  userDBMgr,
		Logger:         logger,
	})
	mcpSrv := seamMCP.New(seamMCP.Config{
		AgentService:   agentSvc,
		TaskService:    taskSvc,
		WebhookService: webhookSvc,
		ToolCallLogger: agentSvc,
		Logger:         logger,
	})
	mcpHandler := mcpSrv.Handler(jwtMgr)

	// Create assistant (agentic AI with tool use).
	var assistantHandler *assistant.Handler
	if chatCompleter != nil {
		// All built-in providers implement ai.ToolChatCompleter, but a
		// future provider that only satisfies ai.ChatCompleter would
		// silently disable the assistant. Surface that with a warning
		// so operators see why /api/assistant routes are missing.
		toolCompleter, ok := chatCompleter.(ai.ToolChatCompleter)
		if !ok {
			logger.Warn("assistant disabled: chat completer does not support tool use")
		}
		if ok {
			assistantStore := assistant.NewStore()
			memoryStore := assistant.NewMemoryStore()
			profileStore := assistant.NewProfileStore()
			toolRegistry := assistant.NewToolRegistry()
			assistant.RegisterDefaultTools(toolRegistry, noteSvc, taskSvc, projectSvc, searchSvc, graphSvc, chatHistorySvc)

			assistantModel := cfg.Assistant.Model
			if assistantModel == "" {
				assistantModel = cfg.Models.Chat
			}

			assistantSvc := assistant.NewService(assistant.ServiceDeps{
				Store:        assistantStore,
				MemoryStore:  memoryStore,
				ProfileStore: profileStore,
				Registry:     toolRegistry,
				LLM:          toolCompleter,
				// All built-in providers also implement plain
				// ChatCompleter, so the same client doubles as the
				// summarizer for conversation digests.
				Summarizer:    chatCompleter,
				ChatModel:     assistantModel,
				UserDBManager: userDBMgr,
				Hub:           hub,
				Logger:        logger,
				Config: assistant.ServiceConfig{
					MaxIterations:        cfg.Assistant.MaxIterations,
					ConfirmationRequired: cfg.Assistant.ConfirmationRequired,
				},
			})
			// Register memory/profile tools (needs the service for callbacks).
			assistant.RegisterMemoryTools(toolRegistry, assistantSvc)
			assistantHandler = assistant.NewHandler(assistantSvc, logger)
			logger.Info("assistant enabled", "model", assistantModel, "max_iterations", cfg.Assistant.MaxIterations)
		}
	}

	// Create scheduler + briefing services. The scheduler runs proactive
	// jobs (daily briefing today; reminders/automations in later phases).
	var scheduleHandler *scheduler.Handler
	var schedSvc *scheduler.Service
	var schedDone chan struct{}
	schedulerEnabled := cfg.Scheduler.Enabled == nil || *cfg.Scheduler.Enabled
	if schedulerEnabled {
		scheduleStore := scheduler.NewStore()
		schedSvc = scheduler.NewService(scheduler.Config{
			Store:        scheduleStore,
			DBManager:    userDBMgr,
			Logger:       logger,
			TickInterval: cfg.Scheduler.TickInterval.Duration,
		})

		briefingSvc := briefing.NewService(briefing.Config{
			NoteService:    noteSvc,
			ProjectService: projectSvc,
			TaskService:    taskSvc,
			DBManager:      userDBMgr,
			Hub:            hub,
			Logger:         logger,
		})
		schedSvc.RegisterRunner(scheduler.ActionBriefing, briefingSvc.Action())

		// Auto-provision the daily briefing schedule on first startup so
		// new installs get a useful default. We deduplicate by name to
		// avoid stacking duplicates if the server restarts.
		if cfg.Scheduler.DailyBriefing.Enabled == nil || *cfg.Scheduler.DailyBriefing.Enabled {
			if err := provisionDailyBriefing(ctx, schedSvc, cfg.Scheduler.DailyBriefing, logger); err != nil {
				logger.Warn("failed to provision daily briefing schedule", "error", err)
			}
		}

		scheduleHandler = scheduler.NewHandler(schedSvc, logger)

		// Start the scheduler tick loop. Errors are logged inside Run.
		// schedDone is closed when the scheduler goroutine returns so the
		// shutdown sequence can wait for an in-flight tick (e.g. a
		// briefing.Generate writing to the user DB) to finish before the
		// deferred CloseAll fires.
		schedDone = make(chan struct{})
		go func() {
			defer close(schedDone)
			if err := schedSvc.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
				logger.Error("scheduler stopped", "error", err)
			}
		}()
		logger.Info("scheduler enabled",
			"tick_interval", cfg.Scheduler.TickInterval.Duration,
			"daily_briefing_cron", cfg.Scheduler.DailyBriefing.CronExpr)
	}

	// Create and start the HTTP server.
	srv := server.New(server.Config{
		Listen:           cfg.Listen,
		Logger:           logger,
		JWTManager:       jwtMgr,
		Hub:              hub,
		CORSOrigins:      cfg.CORSOrigins,
		WebDistDir:       cfg.WebDistDir,
		AuthHandler:      authHandler,
		ProjectHandler:   projectHandler,
		NoteHandler:      noteHandler,
		SearchHandler:    searchHandler,
		AIHandler:        aiHandler,
		CaptureHandler:   captureHandler,
		TemplateHandler:  templateHandler,
		GraphHandler:     graphHandler,
		SettingsHandler:  settingsHandler,
		ChatHandler:      chatHistoryHandler,
		TaskHandler:      taskHandler,
		ReviewHandler:    reviewHandler,
		WebhookHandler:   webhookHandler,
		AssistantHandler: assistantHandler,
		ScheduleHandler:  scheduleHandler,
		WSMessageHandler: wsHandler,
		MCPHandler:       mcpHandler,
	})

	// Start server in a goroutine.
	errCh := make(chan error, 1)
	go func() {
		errCh <- srv.Start()
	}()

	// Wait for shutdown signal or server error.
	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-errCh:
		if err != nil {
			return err
		}
	}

	// Graceful shutdown with 10-second timeout.
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	logger.Info("shutting down...")

	// 1. Stop HTTP server (stop accepting new connections, drain in-flight).
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	// 2. Close MCP server (stop background goroutines).
	mcpSrv.Close()

	// 2a. Stop handler background goroutines (rate limiter eviction).
	aiHandler.Close()
	authHandler.Close()

	// 3. Close all WebSocket connections with close frames.
	hub.CloseAll()

	// 4. Wait for AI queue workers to finish before closing databases.
	<-aiQueueDone

	// 4a. Wait for the scheduler goroutine to return so any in-flight
	//     tick (e.g. briefing.Generate writing to the user DB) finishes
	//     before the deferred CloseAll runs and yields "database is
	//     closed" errors.
	if schedDone != nil {
		<-schedDone
	}

	// 5. Stop file watcher (deferred w.Close() handles this -- LIFO before DB).
	// 6. Close database (deferred userDBMgr.CloseAll() handles this).

	logger.Info("shutdown complete")
	return nil
}
