package usage

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/katata/seam/internal/ai"
	"github.com/katata/seam/internal/reqctx"
	"github.com/katata/seam/internal/userdb"
)

// TrackedChatCompleter wraps ai.ChatCompleter with usage tracking.
type TrackedChatCompleter struct {
	inner    ai.ChatCompleter
	tracker  *Tracker
	provider string
	fn       Function
}

// NewTrackedChatCompleter creates a tracked wrapper around a ChatCompleter.
func NewTrackedChatCompleter(inner ai.ChatCompleter, tracker *Tracker, provider string, fn Function) *TrackedChatCompleter {
	return &TrackedChatCompleter{
		inner:    inner,
		tracker:  tracker,
		provider: provider,
		fn:       fn,
	}
}

// ChatCompletion delegates to the inner completer, records usage, and returns.
func (t *TrackedChatCompleter) ChatCompletion(ctx context.Context, model string, messages []ai.ChatMessage) (*ai.ChatResponse, error) {
	userID := resolveUserID(ctx)
	if err := t.tracker.CheckBudget(ctx, userID); err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := t.inner.ChatCompletion(ctx, model, messages)
	elapsed := time.Since(start)

	if err != nil {
		return nil, err
	}

	r := &Record{
		UserID:     userID,
		Function:   t.fn,
		Provider:   t.provider,
		Model:      model,
		IsLocal:    t.provider == "ollama",
		DurationMS: elapsed.Milliseconds(),
	}
	if resp.Usage != nil {
		r.InputTokens = resp.Usage.InputTokens
		r.OutputTokens = resp.Usage.OutputTokens
		r.TotalTokens = resp.Usage.TotalTokens
	} else {
		r.InputTokens = estimateInputTokens(messages)
		r.OutputTokens = estimateTokenCount(resp.Content)
		r.TotalTokens = r.InputTokens + r.OutputTokens
	}

	// Fire-and-forget: tracking failure should not break the caller.
	if trackErr := t.tracker.Track(ctx, r); trackErr != nil {
		t.tracker.logger.Warn("failed to track chat completion usage", "error", trackErr)
	}

	return resp, nil
}

// ChatCompletionStream delegates to the inner completer and wraps the
// channels to track usage when the stream completes.
func (t *TrackedChatCompleter) ChatCompletionStream(ctx context.Context, model string, messages []ai.ChatMessage) (<-chan string, <-chan error) {
	userID := resolveUserID(ctx)
	if err := t.tracker.CheckBudget(ctx, userID); err != nil {
		errCh := make(chan error, 1)
		tokenCh := make(chan string)
		errCh <- err
		close(errCh)
		close(tokenCh)
		return tokenCh, errCh
	}

	start := time.Now()
	innerTokenCh, innerErrCh := t.inner.ChatCompletionStream(ctx, model, messages)

	proxyCh := make(chan string, 64)
	proxyErrCh := make(chan error, 1)

	go func() {
		defer close(proxyCh)
		defer close(proxyErrCh)

		var accumulated strings.Builder
		var streamErr error

		for token := range innerTokenCh {
			accumulated.WriteString(token)
			proxyCh <- token
		}
		for err := range innerErrCh {
			if err != nil {
				streamErr = err
				proxyErrCh <- err
			}
		}

		if streamErr != nil {
			return
		}

		elapsed := time.Since(start)
		r := &Record{
			UserID:       userID,
			Function:     t.fn,
			Provider:     t.provider,
			Model:        model,
			InputTokens:  estimateInputTokens(messages),
			OutputTokens: estimateTokenCount(accumulated.String()),
			IsLocal:      t.provider == "ollama",
			DurationMS:   elapsed.Milliseconds(),
		}
		r.TotalTokens = r.InputTokens + r.OutputTokens

		if trackErr := t.tracker.Track(ctx, r); trackErr != nil {
			t.tracker.logger.Warn("failed to track streaming usage", "error", trackErr)
		}
	}()

	return proxyCh, proxyErrCh
}

// TrackedToolChatCompleter wraps ai.ToolChatCompleter with usage tracking.
type TrackedToolChatCompleter struct {
	inner    ai.ToolChatCompleter
	tracker  *Tracker
	provider string
	fn       Function
}

// NewTrackedToolChatCompleter creates a tracked wrapper around a ToolChatCompleter.
func NewTrackedToolChatCompleter(inner ai.ToolChatCompleter, tracker *Tracker, provider string, fn Function) *TrackedToolChatCompleter {
	return &TrackedToolChatCompleter{
		inner:    inner,
		tracker:  tracker,
		provider: provider,
		fn:       fn,
	}
}

// ChatCompletionWithTools delegates to the inner completer and records usage.
func (t *TrackedToolChatCompleter) ChatCompletionWithTools(ctx context.Context, model string, messages []ai.ToolMessage, tools []ai.ToolDefinition) (*ai.ToolChatResponse, error) {
	userID := resolveUserID(ctx)
	if err := t.tracker.CheckBudget(ctx, userID); err != nil {
		return nil, err
	}

	start := time.Now()
	resp, err := t.inner.ChatCompletionWithTools(ctx, model, messages, tools)
	elapsed := time.Since(start)

	if err != nil {
		return nil, err
	}

	r := &Record{
		UserID:     userID,
		Function:   t.fn,
		Provider:   t.provider,
		Model:      model,
		IsLocal:    t.provider == "ollama",
		DurationMS: elapsed.Milliseconds(),
	}
	if resp.Usage != nil {
		r.InputTokens = resp.Usage.InputTokens
		r.OutputTokens = resp.Usage.OutputTokens
		r.TotalTokens = resp.Usage.TotalTokens
	} else {
		r.InputTokens = estimateToolInputTokens(messages)
		r.OutputTokens = estimateTokenCount(resp.Content)
		r.TotalTokens = r.InputTokens + r.OutputTokens
	}

	if trackErr := t.tracker.Track(ctx, r); trackErr != nil {
		t.tracker.logger.Warn("failed to track tool chat usage", "error", trackErr)
	}

	return resp, nil
}

// ChatCompletionWithToolsStream delegates to the inner completer's
// streaming method and records usage from the final frame. If the inner
// completer does not implement ai.ToolChatStreamer, returns a channel
// that immediately yields an error.
func (t *TrackedToolChatCompleter) ChatCompletionWithToolsStream(ctx context.Context, model string, messages []ai.ToolMessage, tools []ai.ToolDefinition) (<-chan ai.ToolChatDelta, <-chan error) {
	streamer, ok := t.inner.(ai.ToolChatStreamer)
	if !ok {
		deltaCh := make(chan ai.ToolChatDelta)
		errCh := make(chan error, 1)
		errCh <- fmt.Errorf("usage: inner completer does not support streaming")
		close(deltaCh)
		close(errCh)
		return deltaCh, errCh
	}

	userID := resolveUserID(ctx)
	if err := t.tracker.CheckBudget(ctx, userID); err != nil {
		deltaCh := make(chan ai.ToolChatDelta)
		errCh := make(chan error, 1)
		errCh <- err
		close(deltaCh)
		close(errCh)
		return deltaCh, errCh
	}

	innerDeltaCh, innerErrCh := streamer.ChatCompletionWithToolsStream(ctx, model, messages, tools)

	deltaCh := make(chan ai.ToolChatDelta, 64)
	errCh := make(chan error, 1)
	start := time.Now()

	go func() {
		defer close(deltaCh)
		defer close(errCh)

		var final *ai.ToolChatResponse
		for d := range innerDeltaCh {
			if d.Final != nil {
				final = d.Final
			}
			select {
			case deltaCh <- d:
			case <-ctx.Done():
				errCh <- ctx.Err()
				return
			}
		}

		if err := <-innerErrCh; err != nil {
			errCh <- err
			return
		}

		elapsed := time.Since(start)
		r := &Record{
			UserID:     userID,
			Function:   t.fn,
			Provider:   t.provider,
			Model:      model,
			IsLocal:    t.provider == "ollama",
			DurationMS: elapsed.Milliseconds(),
		}
		if final != nil && final.Usage != nil {
			r.InputTokens = final.Usage.InputTokens
			r.OutputTokens = final.Usage.OutputTokens
			r.TotalTokens = final.Usage.TotalTokens
		} else if final != nil {
			r.InputTokens = estimateToolInputTokens(messages)
			r.OutputTokens = estimateTokenCount(final.Content)
			r.TotalTokens = r.InputTokens + r.OutputTokens
		}
		if trackErr := t.tracker.Track(ctx, r); trackErr != nil {
			t.tracker.logger.Warn("failed to track tool chat stream usage", "error", trackErr)
		}
	}()

	return deltaCh, errCh
}

// TrackedEmbedder wraps ai.EmbeddingGenerator with usage tracking.
type TrackedEmbedder struct {
	inner    ai.EmbeddingGenerator
	tracker  *Tracker
	provider string
}

// NewTrackedEmbedder creates a tracked wrapper around an EmbeddingGenerator.
func NewTrackedEmbedder(inner ai.EmbeddingGenerator, tracker *Tracker, provider string) *TrackedEmbedder {
	return &TrackedEmbedder{
		inner:    inner,
		tracker:  tracker,
		provider: provider,
	}
}

// GenerateEmbedding delegates to the inner embedder and records estimated usage.
func (t *TrackedEmbedder) GenerateEmbedding(ctx context.Context, model, text string) ([]float64, error) {
	userID := resolveUserID(ctx)
	if err := t.tracker.CheckBudget(ctx, userID); err != nil {
		return nil, err
	}

	start := time.Now()
	vec, err := t.inner.GenerateEmbedding(ctx, model, text)
	elapsed := time.Since(start)

	if err != nil {
		return nil, err
	}

	inputTokens := estimateTokenCount(text)
	r := &Record{
		UserID:      userID,
		Function:    FuncEmbedding,
		Provider:    t.provider,
		Model:       model,
		InputTokens: inputTokens,
		TotalTokens: inputTokens,
		IsLocal:     t.provider == "ollama",
		DurationMS:  elapsed.Milliseconds(),
	}

	if trackErr := t.tracker.Track(ctx, r); trackErr != nil {
		t.tracker.logger.Warn("failed to track embedding usage", "error", trackErr)
	}

	return vec, nil
}

// Compile-time interface checks.
var (
	_ ai.ChatCompleter     = (*TrackedChatCompleter)(nil)
	_ ai.ToolChatCompleter = (*TrackedToolChatCompleter)(nil)
	_ ai.ToolChatStreamer   = (*TrackedToolChatCompleter)(nil)
	_ ai.EmbeddingGenerator = (*TrackedEmbedder)(nil)
)

// resolveUserID gets the user ID from context, falling back to DefaultUserID.
func resolveUserID(ctx context.Context) string {
	uid := reqctx.UserIDFromContext(ctx)
	if uid == "" {
		uid = userdb.DefaultUserID
	}
	return uid
}

// estimateTokenCount estimates token count from text using a rough
// 4-characters-per-token heuristic. Returns at least 1 for non-empty text.
func estimateTokenCount(text string) int {
	if text == "" {
		return 0
	}
	return max(len([]rune(text))/4, 1)
}

// estimateInputTokens estimates input tokens from a ChatMessage slice.
func estimateInputTokens(messages []ai.ChatMessage) int {
	total := 0
	for _, m := range messages {
		total += estimateTokenCount(m.Content)
	}
	return total
}

// estimateToolInputTokens estimates input tokens from a ToolMessage slice.
func estimateToolInputTokens(messages []ai.ToolMessage) int {
	total := 0
	for _, m := range messages {
		total += estimateTokenCount(m.Content)
	}
	return total
}
