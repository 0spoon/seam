package main

import (
	"context"
	"encoding/json"
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

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/capture"
	"github.com/katata/seam/internal/chat"
	"github.com/katata/seam/internal/config"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/server"
	"github.com/katata/seam/internal/settings"
	"github.com/katata/seam/internal/template"
	"github.com/katata/seam/internal/userdb"
	"github.com/katata/seam/internal/watcher"
	"github.com/katata/seam/internal/ws"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintf(os.Stderr, "seamd: %v\n", err)
		os.Exit(1)
	}
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

	// Open server.db (shared database for users and refresh tokens).
	serverDBPath := cfg.DataDir + "/server.db"
	serverDB, err := auth.OpenServerDB(serverDBPath)
	if err != nil {
		return fmt.Errorf("open server db: %w", err)
	}
	defer serverDB.Close()

	// Create per-user database manager.
	userDBMgr := userdb.NewSQLManager(
		cfg.DataDir,
		cfg.UserDB.EvictionTimeout.Duration,
		logger,
	)
	// NOTE: userDBMgr.CloseAll is deferred here but watcher.Close and
	// aiQueue shutdown are deferred AFTER this (below), so in LIFO order
	// the watcher and AI queue stop before the DBs are closed.
	defer userDBMgr.CloseAll()

	// Set up context with signal handling for graceful shutdown.
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Start userdb eviction loop.
	go userDBMgr.Run(ctx)

	// Create auth components.
	authStore := auth.NewSQLStore(serverDB)
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

	// C-19: Wire frontmatter updater so project cascade-to-inbox clears
	// the project field from YAML frontmatter on disk.
	projectSvc.SetFrontmatterUpdater(func(notesDir, filePath string) error {
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
		return os.WriteFile(absPath, []byte(content), 0o644)
	})

	// Create search components.
	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)
	searchHandler := search.NewHandler(searchSvc, logger)

	// Create capture components.
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

	// Create AI components (Ollama, ChromaDB, embedder, chat, synthesizer, linker, writer).
	ollamaClient := ai.NewOllamaClient(
		cfg.OllamaBaseURL,
		cfg.AI.EmbeddingTimeout.Duration,
		cfg.AI.ChatTimeout.Duration,
	)

	var aiHandler *ai.Handler
	var aiQueue *ai.Queue
	var chatSvc *ai.ChatService
	var synthSvc *ai.Synthesizer

	// Create WebSocket hub.
	hub := ws.NewHub(logger)

	// Wire up AI components (only if ChromaDB is configured).
	taskStore := ai.NewTaskStore()
	aiQueue = ai.NewQueue(taskStore, userDBMgr, hub, cfg.AI.QueueWorkers, logger)

	var embedder *ai.Embedder
	var chromaClient *ai.ChromaClient

	if cfg.ChromaDBURL != "" {
		chromaClient = ai.NewChromaClient(cfg.ChromaDBURL)
		embedder = ai.NewEmbedder(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)
		chatSvc = ai.NewChatService(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Chat, logger)
		synthSvc = ai.NewSynthesizer(ollamaClient, userDBMgr, cfg.Models.Chat, logger)
		linker := ai.NewAutoLinker(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Background, hub, logger)

		// Register task handlers.
		aiQueue.RegisterHandler(ai.TaskTypeEmbed, embedder.HandleEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeDeleteEmbed, embedder.HandleDeleteEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeChat, chatSvc.HandleChatTask)
		aiQueue.RegisterHandler(ai.TaskTypeSynthesize, synthSvc.HandleSynthesizeTask)
		aiQueue.RegisterHandler(ai.TaskTypeAutolink, linker.HandleAutolinkTask)

		// Create AI writer (uses chat model for writing assist).
		aiWriter := ai.NewWriter(ollamaClient, userDBMgr, cfg.Models.Chat, logger)
		bodyAdapter := &noteBodyAdapter{noteSvc: noteSvc}
		aiWriter.SetNoteBodyLoader(bodyAdapter)
		aiWriter.SetNoteBodyUpdater(bodyAdapter)
		aiQueue.RegisterHandler(ai.TaskTypeAssist, aiWriter.HandleAssistTask)
		aiQueue.RegisterHandler(ai.TaskTypeSummarizeTranscript, aiWriter.HandleSummarizeTranscriptTask)

		// Wire background summarization for voice captures.
		captureSvc.SetSummarizeFunc(func(ctx context.Context, userID, noteID string) {
			payload, _ := json.Marshal(ai.SummarizeTranscriptPayload{NoteID: noteID})
			if err := aiQueue.Enqueue(ctx, &ai.Task{
				UserID:   userID,
				Type:     ai.TaskTypeSummarizeTranscript,
				Priority: ai.PriorityBackground,
				Payload:  payload,
			}); err != nil {
				logger.Warn("failed to enqueue transcript summarization", "note_id", noteID, "error", err)
			}
		})

		// Enable semantic search.
		semanticSearcher := search.NewSemanticSearcher(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)
		searchSvc.SetSemanticSearcher(semanticSearcher)

		// Create AI suggester for tag/project suggestions.
		suggester := ai.NewSuggester(ollamaClient, cfg.Models.Chat, logger)

		aiHandler = ai.NewHandler(aiQueue, chatSvc, synthSvc, linker, embedder, aiWriter, suggester, userDBMgr, logger)
		logger.Info("AI features enabled", "ollama_url", cfg.OllamaBaseURL, "chromadb_url", cfg.ChromaDBURL)
	} else {
		// Even without ChromaDB, writer and suggester can work with just Ollama.
		aiWriter := ai.NewWriter(ollamaClient, userDBMgr, cfg.Models.Chat, logger)
		bodyAdapter := &noteBodyAdapter{noteSvc: noteSvc}
		aiWriter.SetNoteBodyLoader(bodyAdapter)
		aiWriter.SetNoteBodyUpdater(bodyAdapter)
		suggester := ai.NewSuggester(ollamaClient, cfg.Models.Chat, logger)
		aiHandler = ai.NewHandler(nil, nil, nil, nil, nil, aiWriter, suggester, userDBMgr, logger)
		logger.Info("AI features: ChromaDB not configured; only writing assist and suggestions available")
	}

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
		absPath := notesDir + "/" + filePath
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
		payload, _ := json.Marshal(ws.NoteChangedPayload{
			NoteID:     noteID,
			ChangeType: changeType,
		})
		hub.Send(uid, ws.Message{Type: ws.MsgTypeNoteChanged, Payload: payload})

		// Enqueue embedding tasks if AI is enabled.
		// C-32: Log enqueue errors instead of silently discarding them.
		if embedder != nil && aiQueue != nil {
			switch changeType {
			case "created", "modified":
				embedPayload, _ := json.Marshal(ai.EmbedPayload{NoteID: noteID})
				if err := aiQueue.Enqueue(fctx, &ai.Task{
					UserID:   uid,
					Type:     ai.TaskTypeEmbed,
					Priority: ai.PriorityBackground,
					Payload:  embedPayload,
				}); err != nil {
					logger.Warn("failed to enqueue embed task", "note_id", noteID, "error", err)
				}
			case "deleted":
				deletePayload, _ := json.Marshal(ai.DeleteEmbedPayload{NoteID: noteID})
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

	// Start AI task queue (load pending tasks, then run workers).
	// aiQueueDone is closed when the queue workers finish, so the shutdown
	// sequence can wait for in-flight tasks before closing databases.
	aiQueueDone := make(chan struct{})
	if aiQueue != nil {
		if err := aiQueue.LoadPending(ctx); err != nil {
			logger.Warn("failed to load pending AI tasks", "error", err)
		}
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
						conn.Write(writeCtx, websocket.MessageText, data)
						cancel()
						return
					}
				}

				tokenCh, citations, errCh := chatSvc.AskStream(fctx, uid, payload.Query, payload.History)

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
					conn.Write(writeCtx, websocket.MessageText, data)
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
					conn.Write(writeCtx, websocket.MessageText, data)
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
		ReviewHandler:    reviewHandler,
		WSMessageHandler: wsHandler,
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

	// 2. Close all WebSocket connections with close frames.
	hub.CloseAll()

	// 3. Wait for AI queue workers to finish before closing databases.
	<-aiQueueDone

	// 4. Stop file watcher (deferred w.Close() handles this -- LIFO before DBs).
	// 5. Close all user databases (deferred userDBMgr.CloseAll() handles this).
	// 6. Close server database (deferred serverDB.Close() handles this).

	logger.Info("shutdown complete")
	return nil
}
