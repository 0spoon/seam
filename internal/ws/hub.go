// Package ws manages WebSocket connections per user and broadcasts messages.
package ws

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/coder/websocket"
)

// writeTimeout is the maximum duration for writing a message to a connection.
const writeTimeout = 5 * time.Second

// maxConnsPerUser is the maximum number of concurrent WebSocket connections
// allowed per user. Additional connections are rejected.
const maxConnsPerUser = 64

// Message represents a WebSocket message.
type Message struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
}

// Hub manages WebSocket connections grouped by user ID.
type Hub struct {
	mu     sync.RWMutex
	conns  map[string]map[*websocket.Conn]struct{} // userID -> set of connections
	logger *slog.Logger

	// shutCtx is cancelled during CloseAll to abort in-flight writes.
	shutCtx    context.Context
	shutCancel context.CancelFunc
}

// NewHub creates a new Hub.
func NewHub(logger *slog.Logger) *Hub {
	if logger == nil {
		logger = slog.Default()
	}
	ctx, cancel := context.WithCancel(context.Background())
	return &Hub{
		conns:      make(map[string]map[*websocket.Conn]struct{}),
		logger:     logger,
		shutCtx:    ctx,
		shutCancel: cancel,
	}
}

// ErrTooManyConns is returned when a user exceeds the per-user connection limit.
var ErrTooManyConns = errors.New("too many WebSocket connections")

// Register adds a WebSocket connection for the given user.
// Returns ErrTooManyConns if the user already has maxConnsPerUser connections.
func (h *Hub) Register(userID string, conn *websocket.Conn) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	if h.conns[userID] == nil {
		h.conns[userID] = make(map[*websocket.Conn]struct{})
	}
	if len(h.conns[userID]) >= maxConnsPerUser {
		return ErrTooManyConns
	}
	h.conns[userID][conn] = struct{}{}
	h.logger.Debug("ws.Hub.Register: connection registered",
		"user_id", userID,
		"user_conns", len(h.conns[userID]),
	)
	return nil
}

// Unregister removes a WebSocket connection for the given user.
// It is safe to call multiple times for the same connection.
func (h *Hub) Unregister(userID string, conn *websocket.Conn) {
	h.mu.Lock()
	defer h.mu.Unlock()

	conns, ok := h.conns[userID]
	if !ok {
		return
	}
	delete(conns, conn)
	if len(conns) == 0 {
		delete(h.conns, userID)
	}
	h.logger.Debug("ws.Hub.Unregister: connection unregistered",
		"user_id", userID,
	)
}

// Send sends a message to all connections for the given user.
// Dead connections are automatically cleaned up on write failure.
func (h *Hub) Send(userID string, msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ws.Hub.Send: marshal: %w", err)
	}

	h.mu.RLock()
	conns, ok := h.conns[userID]
	if !ok || len(conns) == 0 {
		h.mu.RUnlock()
		return nil
	}
	// Copy the set so we can release the read lock before writing.
	targets := make([]*websocket.Conn, 0, len(conns))
	for c := range conns {
		targets = append(targets, c)
	}
	h.mu.RUnlock()

	var dead []*websocket.Conn
	for _, c := range targets {
		ctx, cancel := context.WithTimeout(h.shutCtx, writeTimeout)
		wErr := c.Write(ctx, websocket.MessageText, data)
		cancel()
		if wErr != nil {
			h.logger.Warn("ws.Hub.Send: write failed, removing dead connection",
				"user_id", userID,
				"error", wErr,
			)
			dead = append(dead, c)
		}
	}

	// Clean up dead connections outside the write loop.
	for _, c := range dead {
		h.Unregister(userID, c)
		_ = c.CloseNow()
	}

	return nil
}

// Broadcast sends a message to all connected users.
// Dead connections are automatically cleaned up on write failure.
func (h *Hub) Broadcast(msg Message) error {
	data, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("ws.Hub.Broadcast: marshal: %w", err)
	}

	h.mu.RLock()
	type target struct {
		userID string
		conn   *websocket.Conn
	}
	var targets []target
	for uid, conns := range h.conns {
		for c := range conns {
			targets = append(targets, target{userID: uid, conn: c})
		}
	}
	h.mu.RUnlock()

	var dead []target
	for _, t := range targets {
		ctx, cancel := context.WithTimeout(h.shutCtx, writeTimeout)
		wErr := t.conn.Write(ctx, websocket.MessageText, data)
		cancel()
		if wErr != nil {
			h.logger.Warn("ws.Hub.Broadcast: write failed, removing dead connection",
				"user_id", t.userID,
				"error", wErr,
			)
			dead = append(dead, t)
		}
	}

	for _, t := range dead {
		h.Unregister(t.userID, t.conn)
		_ = t.conn.CloseNow()
	}

	return nil
}

// ConnectionCount returns the total number of active WebSocket connections.
func (h *Hub) ConnectionCount() int {
	h.mu.RLock()
	defer h.mu.RUnlock()

	count := 0
	for _, conns := range h.conns {
		count += len(conns)
	}
	return count
}

// CloseAll sends a close frame to all connected WebSocket clients and
// unregisters them. This should be called during graceful shutdown.
func (h *Hub) CloseAll() {
	// Cancel the shutdown context to abort any in-flight writes.
	h.shutCancel()

	h.mu.Lock()
	defer h.mu.Unlock()

	for userID, conns := range h.conns {
		for c := range conns {
			if err := c.Close(websocket.StatusGoingAway, "server shutting down"); err != nil {
				h.logger.Debug("ws.Hub.CloseAll: close failed",
					"user_id", userID, "error", err)
			}
		}
	}

	h.conns = make(map[string]map[*websocket.Conn]struct{})
}
