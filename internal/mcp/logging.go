package mcp

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"
	"github.com/oklog/ulid/v2"

	"github.com/katata/seam/internal/agent"
	"github.com/katata/seam/internal/reqctx"
)

// ToolCallLogger persists tool call audit records to the database.
type ToolCallLogger interface {
	LogToolCall(ctx context.Context, userID string, tc *agent.ToolCallRecord) error
}

// loggingMiddleware returns a tool handler middleware that logs every tool call
// via structured logging and persists the audit record to the database.
func (s *Server) loggingMiddleware() mcpserver.ToolHandlerMiddleware {
	return func(next mcpserver.ToolHandlerFunc) mcpserver.ToolHandlerFunc {
		return func(ctx context.Context, req mcp.CallToolRequest) (*mcp.CallToolResult, error) {
			start := time.Now()

			result, err := next(ctx, req)

			duration := time.Since(start).Milliseconds()
			userID := reqctx.UserIDFromContext(ctx)

			// Build log entry.
			argsJSON := "{}"
			if args := req.GetArguments(); args != nil {
				if b, jsonErr := json.Marshal(args); jsonErr == nil {
					argsJSON = string(b)
				}
			}

			var resultText, errText string
			// Redact sensitive tool results (e.g., webhook secrets) from audit log.
			sensitiveTools := map[string]bool{"webhook_register": true}

			if err != nil {
				errText = err.Error()
			} else if result != nil && result.IsError {
				// Extract error text from content.
				for _, c := range result.Content {
					if tc, ok := c.(mcp.TextContent); ok {
						errText = tc.Text
						break
					}
				}
			} else if result != nil {
				// Extract text from content (truncate for logging).
				for _, c := range result.Content {
					if tc, ok := c.(mcp.TextContent); ok {
						resultText = tc.Text
						if sensitiveTools[req.Params.Name] {
							resultText = "[REDACTED]"
						} else if len(resultText) > 1000 {
							resultText = resultText[:1000] + "..."
						}
						break
					}
				}
			}

			s.logger.Info("mcp tool call",
				"user_id", userID,
				"tool", req.Params.Name,
				"duration_ms", duration,
				"error", errText,
			)

			// Persist to database if logger is available.
			if s.cfg.ToolCallLogger != nil && userID != "" {
				auditID, idErr := ulid.New(ulid.Now(), rand.Reader)
				if idErr != nil {
					s.logger.Warn("failed to generate audit record ID", "error", idErr)
					return result, err
				}
				tc := &agent.ToolCallRecord{
					ID:         auditID.String(),
					ToolName:   req.Params.Name,
					Arguments:  argsJSON,
					Result:     resultText,
					Error:      errText,
					DurationMs: duration,
					CreatedAt:  time.Now().UTC(),
				}
				if logErr := s.cfg.ToolCallLogger.LogToolCall(ctx, userID, tc); logErr != nil {
					s.logger.Warn("failed to persist tool call audit",
						"tool", req.Params.Name,
						"error", logErr,
					)
				}
			}

			return result, err
		}
	}
}
