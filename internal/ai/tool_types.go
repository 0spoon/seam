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
	Content      string      `json:"content"`
	ToolCalls    []ToolCall  `json:"tool_calls,omitempty"`
	FinishReason string      `json:"finish_reason"` // "stop", "tool_calls", "length"
	Usage        *TokenUsage `json:"usage,omitempty"`
}

// ToolChatCompleter extends ChatCompleter with tool/function calling support.
// Implementations: OllamaClient, OpenAIClient, AnthropicClient.
// The base ChatCompleter interface is preserved for backward compatibility.
type ToolChatCompleter interface {
	ChatCompletionWithTools(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition) (*ToolChatResponse, error)
}

// ToolChatDelta is one frame emitted by a streaming tool-chat call. Each
// frame carries either an incremental text chunk (TextDelta) or the final
// aggregated response (Final). The streamer emits zero or more text-only
// frames followed by exactly one Final frame before closing the channel.
// When the LLM decides to call tools instead of replying with text, the
// streamer emits zero text frames and a single Final frame whose ToolCalls
// are populated.
type ToolChatDelta struct {
	TextDelta string
	Final     *ToolChatResponse
}

// ToolChatStreamer extends ToolChatCompleter with incremental text
// streaming. Implementations that satisfy this interface stream text
// tokens as they arrive from the provider, while still returning an
// authoritative ToolChatResponse on the final frame.
//
// Implementations: OllamaClient, OpenAIClient. AnthropicClient does not
// yet implement this interface; callers should type-assert and fall back
// to ChatCompletionWithTools when the assertion fails.
type ToolChatStreamer interface {
	ChatCompletionWithToolsStream(ctx context.Context, model string, messages []ToolMessage, tools []ToolDefinition) (<-chan ToolChatDelta, <-chan error)
}

// Compile-time interface checks.
var (
	_ ToolChatCompleter = (*OllamaClient)(nil)
	_ ToolChatCompleter = (*OpenAIClient)(nil)
	_ ToolChatCompleter = (*AnthropicClient)(nil)

	_ ToolChatStreamer = (*OllamaClient)(nil)
	_ ToolChatStreamer = (*OpenAIClient)(nil)
)
