package ai

import (
	"context"
	"encoding/json"
)

// ToolDefinition describes a tool that the LLM can invoke.
type ToolDefinition struct {
	Name        string          `json:"name"`
	Description string          `json:"description"`
	Parameters  json.RawMessage `json:"parameters"` // JSON Schema
}

// ToolCall represents a tool invocation requested by the LLM.
type ToolCall struct {
	ID        string `json:"id"`
	Name      string `json:"name"`
	Arguments string `json:"arguments"` // JSON string
}

// ToolMessage is a ChatMessage with tool-calling extensions.
// It extends the base ChatMessage to support function calling across providers.
type ToolMessage struct {
	Role       string     `json:"role"` // "system", "user", "assistant", "tool"
	Content    string     `json:"content"`
	ToolCalls  []ToolCall `json:"tool_calls,omitempty"`   // present when role == "assistant" and LLM wants to call tools
	ToolCallID string     `json:"tool_call_id,omitempty"` // present when role == "tool" (result of a tool call)
	Name       string     `json:"name,omitempty"`         // tool name, present when role == "tool"
}

// ToolChatResponse extends ChatResponse with tool call information.
type ToolChatResponse struct {
	Content      string     `json:"content"`
	ToolCalls    []ToolCall `json:"tool_calls,omitempty"`
	FinishReason string     `json:"finish_reason"` // "stop", "tool_calls", "length"
}

// ToolChatCompleter extends ChatCompleter with tool/function calling support.
// Implementations: OllamaClient, OpenAIClient, AnthropicClient.
// The base ChatCompleter interface is preserved for backward compatibility.
type ToolChatCompleter interface {
	ChatCompletionWithTools(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition) (*ToolChatResponse, error)
}

// Compile-time interface checks.
var (
	_ ToolChatCompleter = (*OllamaClient)(nil)
	_ ToolChatCompleter = (*OpenAIClient)(nil)
	_ ToolChatCompleter = (*AnthropicClient)(nil)
)
