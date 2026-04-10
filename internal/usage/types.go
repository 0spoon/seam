// Package usage tracks token consumption across AI providers and functions.
// It provides decorator wrappers for ai.ChatCompleter, ai.ToolChatCompleter,
// and ai.EmbeddingGenerator that transparently record usage to SQLite.
package usage

import "time"

// Function categorizes the purpose of an AI call.
type Function string

const (
	FuncChat            Function = "chat"
	FuncChatSummarize   Function = "chat_summarize"
	FuncAssistant       Function = "assistant"
	FuncAssistExpand    Function = "assist_expand"
	FuncAssistSummarize Function = "assist_summarize"
	FuncAssistExtract   Function = "assist_extract"
	FuncSynthesis       Function = "synthesis"
	FuncAutolink        Function = "autolink"
	FuncSuggestTags     Function = "suggest_tags"
	FuncSuggestProject  Function = "suggest_project"
	FuncLibrarian       Function = "librarian"
	FuncEmbedding       Function = "embedding"
	FuncTranscription   Function = "transcript_summarize"
)

// Record is a single token usage entry persisted to the token_usage table.
type Record struct {
	ID             string
	UserID         string
	Function       Function
	Provider       string // "ollama", "openai", "anthropic"
	Model          string // actual model name (e.g. "gpt-4o-mini", "llama3:8b")
	InputTokens    int
	OutputTokens   int
	TotalTokens    int
	IsLocal        bool
	DurationMS     int64
	ConversationID string // optional, groups assistant turns
	CreatedAt      time.Time
}

// Summary holds aggregated token counts for a time range.
type Summary struct {
	TotalTokens  int64 `json:"total_tokens"`
	InputTokens  int64 `json:"input_tokens"`
	OutputTokens int64 `json:"output_tokens"`
	BilledTokens int64 `json:"billed_tokens"` // excludes local
	LocalTokens  int64 `json:"local_tokens"`
	CallCount    int64 `json:"call_count"`
}

// FunctionUsage holds per-function aggregated counts.
type FunctionUsage struct {
	Function     string `json:"function"`
	TotalTokens  int64  `json:"total_tokens"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CallCount    int64  `json:"call_count"`
}

// ProviderUsage holds per-provider aggregated counts.
type ProviderUsage struct {
	Provider     string `json:"provider"`
	TotalTokens  int64  `json:"total_tokens"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CallCount    int64  `json:"call_count"`
}

// ModelUsage holds per-model aggregated counts.
type ModelUsage struct {
	Model        string `json:"model"`
	Provider     string `json:"provider"`
	TotalTokens  int64  `json:"total_tokens"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
	CallCount    int64  `json:"call_count"`
}

// TimeSeriesPoint is a single bucket in a time series.
type TimeSeriesPoint struct {
	Bucket       string `json:"bucket"` // formatted date/hour/month
	TotalTokens  int64  `json:"total_tokens"`
	BilledTokens int64  `json:"billed_tokens"`
	CallCount    int64  `json:"call_count"`
}
