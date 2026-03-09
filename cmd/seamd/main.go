package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/coder/websocket"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/config"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/server"
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

func run() error {
	configPath := flag.String("config", "seam-server.yaml", "path to configuration file")
	flag.Parse()

	// Load configuration.
	cfg, err := config.Load(*configPath)
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	// Set up structured logging.
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelDebug,
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

	// Create project components.
	projectStore := project.NewStore()
	projectSvc := project.NewService(projectStore, userDBMgr, logger)
	projectHandler := project.NewHandler(projectSvc, logger)

	// Create note components.
	noteStore := note.NewSQLStore()
	noteSvc := note.NewService(noteStore, projectStore, userDBMgr, nil, logger) // suppressor set below
	noteHandler := note.NewHandler(noteSvc, logger)

	// Create search components.
	ftsStore := search.NewFTSStore()
	searchSvc := search.NewService(ftsStore, userDBMgr, logger)
	searchHandler := search.NewHandler(searchSvc, logger)

	// Create AI components (Ollama, ChromaDB, embedder, chat, synthesizer, linker).
	ollamaClient := ai.NewOllamaClient(
		cfg.OllamaBaseURL,
		cfg.AI.EmbeddingTimeout.Duration,
		cfg.AI.ChatTimeout.Duration,
	)

	var aiHandler *ai.Handler
	var aiQueue *ai.Queue

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
		chatSvc := ai.NewChatService(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Chat, logger)
		synthesizer := ai.NewSynthesizer(ollamaClient, userDBMgr, cfg.Models.Chat, logger)
		linker := ai.NewAutoLinker(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Background, hub, logger)

		// Register task handlers.
		aiQueue.RegisterHandler(ai.TaskTypeEmbed, embedder.HandleEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeDeleteEmbed, embedder.HandleDeleteEmbedTask)
		aiQueue.RegisterHandler(ai.TaskTypeChat, chatSvc.HandleChatTask)
		aiQueue.RegisterHandler(ai.TaskTypeSynthesize, synthesizer.HandleSynthesizeTask)
		aiQueue.RegisterHandler(ai.TaskTypeAutolink, linker.HandleAutolinkTask)

		// Enable semantic search.
		semanticSearcher := search.NewSemanticSearcher(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, logger)
		searchSvc.SetSemanticSearcher(semanticSearcher)

		aiHandler = ai.NewHandler(aiQueue, chatSvc, synthesizer, linker, embedder, userDBMgr, logger)
		logger.Info("AI features enabled", "ollama_url", cfg.OllamaBaseURL, "chromadb_url", cfg.ChromaDBURL)
	} else {
		logger.Info("AI features disabled (chromadb_url not configured)")
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
		if embedder != nil && aiQueue != nil {
			switch changeType {
			case "created", "modified":
				embedPayload, _ := json.Marshal(ai.EmbedPayload{NoteID: noteID})
				aiQueue.Enqueue(fctx, &ai.Task{
					UserID:   uid,
					Type:     ai.TaskTypeEmbed,
					Priority: ai.PriorityBackground,
					Payload:  embedPayload,
				})
			case "deleted":
				deletePayload, _ := json.Marshal(ai.DeleteEmbedPayload{NoteID: noteID})
				aiQueue.Enqueue(fctx, &ai.Task{
					UserID:   uid,
					Type:     ai.TaskTypeDeleteEmbed,
					Priority: ai.PriorityBackground,
					Payload:  deletePayload,
				})
			}
		}

		return nil
	}

	w, err := watcher.NewWatcher(fileHandler, cfg.Watcher.DebounceInterval.Duration, logger)
	if err != nil {
		return fmt.Errorf("create watcher: %w", err)
	}
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
	if aiQueue != nil {
		if err := aiQueue.LoadPending(ctx); err != nil {
			logger.Warn("failed to load pending AI tasks", "error", err)
		}
		go func() {
			if err := aiQueue.Run(ctx); err != nil {
				logger.Error("AI queue stopped", "error", err)
			}
		}()
	}

	// Build WebSocket message handler for chat.ask.
	var wsHandler ws.MessageHandler
	if cfg.ChromaDBURL != "" && chromaClient != nil {
		chatSvc := ai.NewChatService(ollamaClient, chromaClient, userDBMgr, cfg.Models.Embeddings, cfg.Models.Chat, logger)
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

				tokenCh, citations, errCh := chatSvc.AskStream(fctx, uid, payload.Query, payload.History)

				// Stream tokens to client.
				go func() {
					for token := range tokenCh {
						tokenPayload, _ := json.Marshal(map[string]string{"token": token})
						data, _ := json.Marshal(ws.Message{
							Type:    ws.MsgTypeChatStream,
							Payload: tokenPayload,
						})
						writeCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
						conn.Write(writeCtx, websocket.MessageText, data)
						cancel()
					}

					// Check for errors.
					for err := range errCh {
						if err != nil {
							logger.Error("chat stream error", "error", err)
						}
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

			default:
				logger.Debug("ws: unhandled message type", "type", msg.Type)
			}
		}
	}

	// Create and start the HTTP server.
	srv := server.New(server.Config{
		Listen:           cfg.Listen,
		Logger:           logger,
		JWTManager:       jwtMgr,
		Hub:              hub,
		AuthHandler:      authHandler,
		ProjectHandler:   projectHandler,
		NoteHandler:      noteHandler,
		SearchHandler:    searchHandler,
		AIHandler:        aiHandler,
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

	// 1. Close all WebSocket connections with close frames.
	hub.CloseAll()

	// 2. Stop HTTP server.
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("server shutdown error", "error", err)
	}

	// 3. Stop file watcher (deferred w.Close() handles this).
	// 4. Close all user databases (deferred userDBMgr.CloseAll() handles this).
	// 5. Close server database (deferred serverDB.Close() handles this).

	logger.Info("shutdown complete")
	return nil
}
