package mcp

import (
	"context"
	"encoding/json"
	"time"

	"github.com/mark3labs/mcp-go/mcp"
	mcpserver "github.com/mark3labs/mcp-go/server"

	"github.com/katata/seam/internal/reqctx"
)

// ToolCallLogger defines the interface for logging tool calls.
type ToolCallLogger interface {
	LogToolCall(ctx context.Context, userID, sessionID, toolName, arguments, result, errMsg string, durationMs int64) error
}

// loggingMiddleware returns a tool handler middleware that logs every tool call.
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
				// Extract text from content.
				for _, c := range result.Content {
					if tc, ok := c.(mcp.TextContent); ok {
						resultText = tc.Text
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

			// Log to structured store if available.
			_ = argsJSON
			_ = resultText
			_ = duration

			return result, err
		}
	}
}
