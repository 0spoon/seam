package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"time"

	"github.com/coder/websocket"

	"github.com/katata/seam/internal/auth"
)

// MessageHandler handles incoming WebSocket messages from clients.
// userID is the authenticated user, msg is the parsed message.
type MessageHandler func(ctx context.Context, hub *Hub, conn *websocket.Conn, userID string, msg Message)

// ServeWS returns an http.HandlerFunc that upgrades HTTP connections to
// WebSocket and manages the connection lifecycle.
//
// The client must send an auth message as the first frame:
//
//	{"type": "auth", "payload": {"token": "<jwt>"}}
//
// After successful authentication the connection is registered with the hub
// and a read loop runs until the client disconnects or the context is cancelled.
//
// An optional MessageHandler can process incoming messages (e.g., chat.ask).
// allowedOrigins specifies additional origin patterns for WebSocket upgrades.
// Localhost patterns are always included as defaults.
func ServeWS(hub *Hub, jwtManager *auth.JWTManager, handler MessageHandler, allowedOrigins ...string) http.HandlerFunc {
	origins := []string{"localhost:*", "127.0.0.1:*"}
	origins = append(origins, allowedOrigins...)

	return func(w http.ResponseWriter, r *http.Request) {
		logger := hub.logger.With("remote_addr", r.RemoteAddr)

		conn, err := websocket.Accept(w, r, &websocket.AcceptOptions{
			OriginPatterns: origins,
		})
		if err != nil {
			logger.Error("ws.ServeWS: accept failed", "error", err)
			return
		}
		defer conn.CloseNow()

		// Set an explicit read limit (64KB) to prevent oversized messages.
		conn.SetReadLimit(64 * 1024)

		// Step 1: Read the auth message with a timeout.
		authCtx, authCancel := context.WithTimeout(r.Context(), 10*time.Second)
		defer authCancel()
		_, data, err := conn.Read(authCtx)
		if err != nil {
			logger.Warn("ws.ServeWS: failed to read auth message", "error", err)
			conn.Close(websocket.StatusPolicyViolation, "auth required")
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			logger.Warn("ws.ServeWS: invalid auth message JSON", "error", err)
			conn.Close(websocket.StatusPolicyViolation, "invalid message format")
			return
		}

		if msg.Type != MsgTypeAuth {
			logger.Warn("ws.ServeWS: first message must be auth",
				"got_type", msg.Type,
			)
			conn.Close(websocket.StatusPolicyViolation, "first message must be auth")
			return
		}

		var authPayload AuthPayload
		if err := json.Unmarshal(msg.Payload, &authPayload); err != nil {
			logger.Warn("ws.ServeWS: invalid auth payload", "error", err)
			conn.Close(websocket.StatusPolicyViolation, "invalid auth payload")
			return
		}

		// Step 2: Validate the JWT.
		claims, err := jwtManager.VerifyAccessToken(authPayload.Token)
		if err != nil {
			logger.Warn("ws.ServeWS: authentication failed", "error", err)
			conn.Close(websocket.StatusPolicyViolation, "authentication failed")
			return
		}

		userID := claims.UserID
		logger = logger.With("user_id", userID, "username", claims.Username)
		logger.Info("ws.ServeWS: client authenticated")

		// Step 3: Register and run the read loop with keepalive.
		if err := hub.Register(userID, conn); err != nil {
			logger.Warn("ws.ServeWS: too many connections", "error", err)
			conn.Close(websocket.StatusTryAgainLater, "too many connections")
			return
		}
		defer hub.Unregister(userID, conn)

		// Start ping/pong keepalive. The coder/websocket library supports
		// automatic ping/pong via a context-based read deadline pattern.
		// We use a goroutine that sends pings every 30 seconds.
		pingCtx, pingCancel := context.WithCancel(r.Context())
		defer pingCancel()
		go pingLoop(pingCtx, conn, logger)

		readLoop(r.Context(), hub, conn, userID, logger, handler)
	}
}

// pingInterval is the interval between WebSocket ping frames.
const pingInterval = 30 * time.Second

// pingLoop sends periodic ping frames to detect dead connections.
func pingLoop(ctx context.Context, conn *websocket.Conn, logger *slog.Logger) {
	ticker := time.NewTicker(pingInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			pingCtx, cancel := context.WithTimeout(ctx, 10*time.Second)
			err := conn.Ping(pingCtx)
			cancel()
			if err != nil {
				logger.Debug("ws.pingLoop: ping failed, closing connection", "error", err)
				conn.Close(websocket.StatusGoingAway, "ping timeout")
				return
			}
		}
	}
}

// readLoop reads messages from the WebSocket connection until the client
// disconnects or the context is cancelled.
func readLoop(ctx context.Context, hub *Hub, conn *websocket.Conn, userID string, logger *slog.Logger, handler MessageHandler) {
	for {
		_, data, err := conn.Read(ctx)
		if err != nil {
			// Normal close or context cancellation -- not an error.
			if websocket.CloseStatus(err) == websocket.StatusNormalClosure ||
				websocket.CloseStatus(err) == websocket.StatusGoingAway {
				logger.Debug("ws.readLoop: client disconnected normally")
				return
			}
			if ctx.Err() != nil {
				logger.Debug("ws.readLoop: context cancelled")
				return
			}
			logger.Warn("ws.readLoop: read error", "error", err)
			return
		}

		var msg Message
		if err := json.Unmarshal(data, &msg); err != nil {
			logger.Warn("ws.readLoop: invalid message JSON", "error", err)
			continue
		}

		logger.Debug("ws.readLoop: received message",
			"type", msg.Type,
		)

		if handler != nil {
			handler(ctx, hub, conn, userID, msg)
		} else {
			logger.Debug("ws.readLoop: unhandled message type",
				"type", msg.Type,
			)
		}
	}
}
