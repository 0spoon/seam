// Package server sets up the HTTP server, middleware, and router.
package server

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/cors"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/auth"
	"github.com/katata/seam/internal/capture"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/template"
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
	Listen     string
	Logger     *slog.Logger
	JWTManager *auth.JWTManager
	Hub        *ws.Hub

	AuthHandler      *auth.Handler
	ProjectHandler   *project.Handler
	NoteHandler      *note.Handler
	SearchHandler    *search.Handler
	AIHandler        *ai.Handler
	CaptureHandler   *capture.Handler
	TemplateHandler  *template.Handler
	GraphHandler     *graph.Handler
	WSMessageHandler ws.MessageHandler
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
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"X-Request-ID", "X-Total-Count"},
		AllowCredentials: true,
		MaxAge:           300,
	}))

	// Health check (no auth required).
	r.Get("/api/health", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(`{"status":"ok"}`))
	})

	// Auth routes (no auth required).
	r.Route("/api/auth", func(r chi.Router) {
		r.Mount("/", cfg.AuthHandler.Routes())
	})

	// WebSocket endpoint (auth handled in the WS handshake).
	if cfg.Hub != nil {
		r.Get("/api/ws", ws.ServeWS(cfg.Hub, cfg.JWTManager, cfg.WSMessageHandler))
	}

	// Protected routes.
	r.Group(func(r chi.Router) {
		r.Use(AuthMiddleware(cfg.JWTManager))

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
	})

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
