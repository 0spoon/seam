// Package server sets up the HTTP server, middleware, and router.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/assistant"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/capture"
	"github.com/katata/seam/internal/chat"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/review"
	"github.com/katata/seam/internal/scheduler"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/settings"
	"github.com/katata/seam/internal/task"
	"github.com/katata/seam/internal/template"
	"github.com/katata/seam/internal/usage"
	"github.com/katata/seam/internal/webhook"
	"github.com/katata/seam/internal/ws"
)

// Server is the HTTP server for Seam.
type Server struct {
	httpServer *http.Server
	router     chi.Router
	logger     *slog.Logger
}

// Config holds the dependencies needed to create a Server.
type Config struct {
	Listen      string
	Logger      *slog.Logger
	JWTManager  *auth.JWTManager
	Hub         *ws.Hub
	CORSOrigins []string // allowed CORS origins; defaults to localhost
	WebDistDir  string   // path to web/dist for SPA serving; empty to skip

	AuthHandler      *auth.Handler
	ProjectHandler   *project.Handler
	NoteHandler      *note.Handler
	SearchHandler    *search.Handler
	AIHandler        *ai.Handler
	CaptureHandler   *capture.Handler
	TemplateHandler  *template.Handler
	GraphHandler     *graph.Handler
	SettingsHandler  *settings.Handler
	ChatHandler      *chat.Handler
	TaskHandler      *task.Handler
	ReviewHandler    *review.Handler
	WebhookHandler   *webhook.Handler
	AssistantHandler *assistant.Handler
	ScheduleHandler  *scheduler.Handler
	UsageHandler     *usage.Handler
	WSMessageHandler ws.MessageHandler
	MCPHandler       http.Handler // MCP endpoint handler (optional, mounts at /api/mcp)
}

// New creates a new Server with all routes and middleware configured.
func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = slog.Default()
	}

	r := chi.NewRouter()

	// Global middleware.
	r.Use(RequestIDMiddleware)
	r.Use(RecoveryMiddleware(cfg.Logger))
	r.Use(LoggingMiddleware(cfg.Logger))
	// C-7: Use configurable CORS origins, falling back to localhost.
	corsOrigins := cfg.CORSOrigins
	if len(corsOrigins) == 0 {
		corsOrigins = []string{"http://localhost:*", "http://127.0.0.1:*"}
	}
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   corsOrigins,
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type", "Mcp-Session-Id"},
		ExposedHeaders:   []string{"X-Request-ID", "X-Total-Count", "Mcp-Session-Id"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (no auth required).
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth routes: public (no auth) and protected (auth required) are
	// combined under a single /api/auth mount to avoid chi duplicate-path
	// panics. The AuthHandler.CombinedRoutes method wires both sets and
	// applies the auth middleware only to the protected subset.
	r.Route("/api/auth", func(r chi.Router) {
		r.Mount("/", cfg.AuthHandler.CombinedRoutes(AuthMiddleware(cfg.JWTManager)))
	})

	// WebSocket endpoint (auth handled in the WS handshake).
	if cfg.Hub != nil {
		r.Get("/api/ws", ws.ServeWS(cfg.Hub, cfg.JWTManager, cfg.WSMessageHandler))
	}

	// Protected routes.
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(cfg.JWTManager))

		// Protected auth routes are mounted via CombinedRoutes above.

		if cfg.ProjectHandler != nil {
			r.Route("/api/projects", func(r chi.Router) {
				r.Mount("/", cfg.ProjectHandler.Routes())
			})
		}

		if cfg.NoteHandler != nil {
			r.Route("/api/notes", func(r chi.Router) {
				r.Mount("/", cfg.NoteHandler.Routes())
			})
			r.Route("/api/tags", func(r chi.Router) {
				r.Mount("/", cfg.NoteHandler.TagsRoutes())
			})
		}

		if cfg.SearchHandler != nil {
			r.Route("/api/search", func(r chi.Router) {
				r.Mount("/", cfg.SearchHandler.Routes())
			})
		}

		if cfg.AIHandler != nil {
			r.Route("/api/ai", func(r chi.Router) {
				r.Mount("/", cfg.AIHandler.Routes())
			})
		}

		if cfg.CaptureHandler != nil {
			r.Route("/api/capture", func(r chi.Router) {
				r.Mount("/", cfg.CaptureHandler.Routes())
			})
		}

		if cfg.TemplateHandler != nil {
			r.Route("/api/templates", func(r chi.Router) {
				r.Mount("/", cfg.TemplateHandler.Routes())
			})
		}

		if cfg.GraphHandler != nil {
			r.Route("/api/graph", func(r chi.Router) {
				r.Mount("/", cfg.GraphHandler.Routes())
			})
		}

		if cfg.SettingsHandler != nil {
			r.Route("/api/settings", func(r chi.Router) {
				r.Mount("/", cfg.SettingsHandler.Routes())
			})
		}

		if cfg.ChatHandler != nil {
			r.Route("/api/chat", func(r chi.Router) {
				r.Mount("/", cfg.ChatHandler.Routes())
			})
		}

		if cfg.ReviewHandler != nil {
			r.Route("/api/review", func(r chi.Router) {
				r.Mount("/", cfg.ReviewHandler.Routes())
			})
		}

		if cfg.TaskHandler != nil {
			r.Route("/api/tasks", func(r chi.Router) {
				r.Mount("/", cfg.TaskHandler.Routes())
			})
		}

		if cfg.WebhookHandler != nil {
			r.Route("/api/webhooks", func(r chi.Router) {
				r.Mount("/", cfg.WebhookHandler.Routes())
			})
		}

		if cfg.AssistantHandler != nil {
			r.Route("/api/assistant", func(r chi.Router) {
				r.Mount("/", cfg.AssistantHandler.Routes())
			})
		}

		if cfg.ScheduleHandler != nil {
			r.Route("/api/schedules", func(r chi.Router) {
				r.Mount("/", cfg.ScheduleHandler.Routes())
			})
		}

		if cfg.UsageHandler != nil {
			r.Route("/api/usage", func(r chi.Router) {
				r.Mount("/", cfg.UsageHandler.Routes())
			})
		}
	})

	// MCP endpoint (auth handled internally via mcp-go's HTTPContextFunc).
	if cfg.MCPHandler != nil {
		r.Handle("/api/mcp", cfg.MCPHandler)
	}

	// C-6: Serve the production frontend from web/dist if configured.
	if cfg.WebDistDir != "" {
		if info, err := os.Stat(cfg.WebDistDir); err == nil && info.IsDir() {
			staticFS := http.Dir(cfg.WebDistDir)
			fileServer := http.FileServer(staticFS)

			// Serve static assets directly; for SPA routes, fall back to index.html.
			r.Get("/*", func(w http.ResponseWriter, r *http.Request) {
				// Try to serve the file directly.
				path := r.URL.Path
				if f, err := staticFS.Open(path); err == nil {
					fi, fiErr := f.Stat()
					f.Close()
					// Only serve regular files; skip directories to prevent listings.
					if fiErr == nil && !fi.IsDir() {
						fileServer.ServeHTTP(w, r)
						return
					}
				}
				// SPA fallback: serve index.html for non-API, non-asset paths.
				if !strings.HasPrefix(path, "/api/") {
					http.ServeFile(w, r, filepath.Join(cfg.WebDistDir, "index.html"))
					return
				}
				http.NotFound(w, r)
			})
			cfg.Logger.Info("serving static files", "dir", cfg.WebDistDir)
		}
	}

	return &Server{
		httpServer: &http.Server{
			Addr:         cfg.Listen,
			Handler:      r,
			ReadTimeout:  15 * time.Second,
			WriteTimeout: 30 * time.Second,
			IdleTimeout:  60 * time.Second,
		},
		router: r,
		logger: cfg.Logger,
	}
}

// Router returns the chi router for testing.
func (s *Server) Router() chi.Router {
	return s.router
}

// Start begins listening for HTTP requests.
func (s *Server) Start() error {
	s.logger.Info("starting HTTP server", "addr", s.httpServer.Addr)
	if err := s.httpServer.ListenAndServe(); err != nil && err != http.ErrServerClosed {
		return fmt.Errorf("server.Start: %w", err)
	}
	return nil
}

// Shutdown gracefully stops the server.
func (s *Server) Shutdown(ctx context.Context) error {
	s.logger.Info("shutting down HTTP server")
	return s.httpServer.Shutdown(ctx)
}
