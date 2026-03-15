package agent

import (
	"encoding/json"
	"log/slog"

	"github.com/katata/seam/internal/ws"
)

// HubWSNotifier adapts the WebSocket Hub to the WSNotifier interface.
type HubWSNotifier struct {
	hub    *ws.Hub
	logger *slog.Logger
}

// NewHubWSNotifier creates a WSNotifier backed by a WebSocket Hub.
func NewHubWSNotifier(hub *ws.Hub, logger *slog.Logger) *HubWSNotifier {
	if logger == nil {
		logger = slog.Default()
	}
	return &HubWSNotifier{hub: hub, logger: logger}
}

// SendAgentEvent sends a typed agent event to the user's WebSocket connections.
func (n *HubWSNotifier) SendAgentEvent(userID, eventType string, payload interface{}) {
	data, err := json.Marshal(payload)
	if err != nil {
		n.logger.Warn("agent.HubWSNotifier: marshal payload failed",
			"event", eventType, "error", err)
		return
	}

	msg := ws.Message{
		Type:    eventType,
		Payload: data,
	}
	if err := n.hub.Send(userID, msg); err != nil {
		n.logger.Warn("agent.HubWSNotifier: send failed",
			"event", eventType, "user_id", userID, "error", err)
	}
}
