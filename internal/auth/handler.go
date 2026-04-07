package auth

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/go-chi/chi/v5"
	"golang.org/x/time/rate"

	"github.com/katata/seam/internal/reqctx"
)

// Per-IP rate limiting for auth endpoints: 5 requests per minute, burst of 5.
const (
	authRateLimit = rate.Limit(5.0 / 60.0) // 5 per minute
	authRateBurst = 5
)

// Handler handles HTTP requests for authentication endpoints.
type Handler struct {
	service *Service
	logger  *slog.Logger

	limiterMu sync.Mutex
	limiters  map[string]*ipLimiter
	done      chan struct{} // closed on Close() to stop background goroutines
	closeOnce sync.Once
}

// ipLimiter wraps a rate.Limiter with a last-seen timestamp for eviction.
type ipLimiter struct {
	limiter  *rate.Limiter
	lastSeen time.Time
}

// NewHandler creates a new auth Handler.
func NewHandler(service *Service, logger *slog.Logger) *Handler {
	if logger == nil {
		logger = slog.Default()
	}
	h := &Handler{
		service:  service,
		logger:   logger,
		limiters: make(map[string]*ipLimiter),
		done:     make(chan struct{}),
	}
	go h.evictStaleLimiters()
	return h
}

// Close stops background goroutines (limiter eviction). Safe to call multiple times.
func (h *Handler) Close() {
	h.closeOnce.Do(func() { close(h.done) })
}

// getIPLimiter returns the per-IP rate limiter, creating one if necessary.
func (h *Handler) getIPLimiter(ip string) *rate.Limiter {
	h.limiterMu.Lock()
	defer h.limiterMu.Unlock()
	if entry, ok := h.limiters[ip]; ok {
		entry.lastSeen = time.Now()
		return entry.limiter
	}
	lim := rate.NewLimiter(authRateLimit, authRateBurst)
	h.limiters[ip] = &ipLimiter{limiter: lim, lastSeen: time.Now()}
	return lim
}

// evictStaleLimiters removes limiters that haven't been seen in 10 minutes.
func (h *Handler) evictStaleLimiters() {
	ticker := time.NewTicker(5 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-h.done:
			return
		case <-ticker.C:
			h.limiterMu.Lock()
			cutoff := time.Now().Add(-10 * time.Minute)
			for ip, entry := range h.limiters {
				if entry.lastSeen.Before(cutoff) {
					delete(h.limiters, ip)
				}
			}
			h.limiterMu.Unlock()
		}
	}
}

// authRateLimitMiddleware enforces per-IP rate limiting on public auth endpoints.
func (h *Handler) authRateLimitMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)
		if !h.getIPLimiter(ip).Allow() {
			writeError(w, http.StatusTooManyRequests, "too many requests, try again later")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// clientIP extracts the client IP from the request, stripping the port.
func clientIP(r *http.Request) string {
	ip, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return ip
}

// Routes returns a chi router with all auth routes mounted.
func (h *Handler) Routes() chi.Router {
	r := chi.NewRouter()
	r.Use(h.authRateLimitMiddleware)
	r.Post("/register", h.register)
	r.Post("/login", h.login)
	r.Post("/refresh", h.refresh)
	r.Post("/logout", h.logout)
	return r
}

// ProtectedRoutes returns a chi router with account management routes
// that require authentication middleware.
func (h *Handler) ProtectedRoutes() chi.Router {
	r := chi.NewRouter()
	r.Get("/me", h.getMe)
	r.Put("/password", h.changePassword)
	r.Put("/email", h.updateEmail)
	return r
}

// CombinedRoutes returns a single chi router containing both public auth
// routes and protected routes. The authMiddleware is applied only to the
// protected subset. This avoids mounting two sub-routers on the same path,
// which chi does not allow.
func (h *Handler) CombinedRoutes(authMiddleware func(http.Handler) http.Handler) chi.Router {
	r := chi.NewRouter()

	// Public routes (no auth required, rate-limited per IP).
	r.Group(func(r chi.Router) {
		r.Use(h.authRateLimitMiddleware)
		r.Post("/register", h.register)
		r.Post("/login", h.login)
		r.Post("/refresh", h.refresh)
		r.Post("/logout", h.logout)
	})

	// Protected routes (auth required).
	r.Group(func(r chi.Router) {
		r.Use(authMiddleware)
		r.Get("/me", h.getMe)
		r.Put("/password", h.changePassword)
		r.Put("/email", h.updateEmail)
	})

	return r
}

func (h *Handler) getMe(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	info, err := h.service.GetMe(r.Context(), userID)
	if err != nil {
		h.logger.Error("get me failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, info)
}

func (h *Handler) changePassword(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		CurrentPassword string `json:"current_password"`
		NewPassword     string `json:"new_password"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.service.ChangePassword(r.Context(), userID, req.CurrentPassword, req.NewPassword)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusBadRequest, "current password is incorrect")
			return
		}
		if errors.Is(err, ErrValidation) {
			writeError(w, http.StatusBadRequest, "password must be 8-1024 characters")
			return
		}
		h.logger.Error("change password failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) updateEmail(w http.ResponseWriter, r *http.Request) {
	userID := reqctx.UserIDFromContext(r.Context())
	if userID == "" {
		writeError(w, http.StatusUnauthorized, "missing user identity")
		return
	}

	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req struct {
		Email string `json:"email"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	err := h.service.UpdateEmail(r.Context(), userID, req.Email)
	if err != nil {
		if errors.Is(err, ErrValidation) {
			writeError(w, http.StatusBadRequest, "invalid email format")
			return
		}
		if errors.Is(err, ErrUserExists) {
			writeError(w, http.StatusConflict, "email already in use")
			return
		}
		h.logger.Error("update email failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

func (h *Handler) register(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req RegisterReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.Register(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrRegistrationClosed) {
			// Log at info level: this is the expected response on a
			// configured single-user instance, not an attack signal on
			// its own. Operators investigating noise will see the
			// remote IP via the request log middleware.
			h.logger.Info("registration rejected: owner already exists",
				"remote_addr", r.RemoteAddr)
			writeError(w, http.StatusForbidden, "registration is closed")
			return
		}
		if errors.Is(err, ErrUserExists) {
			writeError(w, http.StatusConflict, "username or email already exists")
			return
		}
		if errors.Is(err, ErrValidation) {
			writeError(w, http.StatusBadRequest, safeRegistrationMessage(err))
			return
		}
		// Keep ErrInvalidCredentials check for backward compatibility.
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusBadRequest, safeRegistrationMessage(err))
			return
		}
		h.logger.Error("registration failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusCreated, resp)
}

func (h *Handler) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req LoginReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	resp, err := h.service.Login(r.Context(), req)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) {
			writeError(w, http.StatusUnauthorized, "invalid credentials")
			return
		}
		h.logger.Error("login failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, resp)
}

func (h *Handler) refresh(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req RefreshReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	tokens, err := h.service.Refresh(r.Context(), req.RefreshToken)
	if err != nil {
		if errors.Is(err, ErrInvalidCredentials) || errors.Is(err, ErrTokenExpired) {
			writeError(w, http.StatusUnauthorized, "invalid or expired refresh token")
			return
		}
		h.logger.Error("token refresh failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	writeJSON(w, http.StatusOK, tokens)
}

func (h *Handler) logout(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<20)
	var req LogoutReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		writeError(w, http.StatusBadRequest, "invalid request body")
		return
	}

	if err := h.service.Logout(r.Context(), req.RefreshToken); err != nil {
		h.logger.Error("logout failed", "error", err)
		writeError(w, http.StatusInternalServerError, "internal server error")
		return
	}

	w.WriteHeader(http.StatusNoContent)
}

// writeJSON encodes v as JSON and writes it to the response.
func writeJSON(w http.ResponseWriter, status int, v interface{}) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(v); err != nil {
		slog.Warn("auth.writeJSON: encode error", "error", err)
	}
}

// writeError writes a JSON error response.
func writeError(w http.ResponseWriter, status int, msg string) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(map[string]string{"error": msg}); err != nil {
		slog.Warn("auth.writeError: encode error", "error", err)
	}
}

// safeRegistrationMessage maps internal validation errors to user-safe messages,
// avoiding leaking implementation details.
func safeRegistrationMessage(err error) string {
	// All auth.Service.Register validation errors wrap ErrValidation and contain
	// a descriptive prefix. Match on the prefix using the error message since
	// the sentinel (ErrValidation) is shared across all validation paths.
	msg := err.Error()
	switch {
	case strings.Contains(msg, "username is required"):
		return "username is required"
	case strings.Contains(msg, "username must be"):
		return "username must be 3-64 characters, alphanumeric/underscore/hyphen only"
	case strings.Contains(msg, "email is required"),
		strings.Contains(msg, "invalid email"):
		return "valid email is required"
	case strings.Contains(msg, "password"):
		return "password must be 8-1024 characters"
	default:
		return "invalid input"
	}
}
