package reqctx

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestUserIDFromContext_WithValue_ReturnsUserID(t *testing.T) {
	ctx := context.WithValue(context.Background(), UserIDKey, "user-123")
	got := UserIDFromContext(ctx)
	require.Equal(t, "user-123", got)
}

func TestUserIDFromContext_EmptyContext_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	got := UserIDFromContext(ctx)
	require.Empty(t, got)
}

func TestRequestIDFromContext_WithValue_ReturnsRequestID(t *testing.T) {
	ctx := context.WithValue(context.Background(), RequestIDKey, "req-abc")
	got := RequestIDFromContext(ctx)
	require.Equal(t, "req-abc", got)
}

func TestRequestIDFromContext_EmptyContext_ReturnsEmpty(t *testing.T) {
	ctx := context.Background()
	got := RequestIDFromContext(ctx)
	require.Empty(t, got)
}

func TestWithUserID_SetsValue(t *testing.T) {
	ctx := WithUserID(context.Background(), "user-456")
	got, ok := ctx.Value(UserIDKey).(string)
	require.True(t, ok)
	require.Equal(t, "user-456", got)
}

func TestWithUsername_SetsValue(t *testing.T) {
	ctx := WithUsername(context.Background(), "alice")
	got, ok := ctx.Value(UsernameKey).(string)
	require.True(t, ok)
	require.Equal(t, "alice", got)
}

func TestWithRequestID_SetsValue(t *testing.T) {
	ctx := WithRequestID(context.Background(), "req-789")
	got, ok := ctx.Value(RequestIDKey).(string)
	require.True(t, ok)
	require.Equal(t, "req-789", got)
}

func TestContextKeys_AreDistinct(t *testing.T) {
	ctx := context.Background()
	ctx = WithUserID(ctx, "user-001")
	ctx = WithUsername(ctx, "bob")
	ctx = WithRequestID(ctx, "req-002")

	require.Equal(t, "user-001", UserIDFromContext(ctx))
	require.Equal(t, "req-002", RequestIDFromContext(ctx))

	// Verify each key holds only its own value.
	got, ok := ctx.Value(UsernameKey).(string)
	require.True(t, ok)
	require.Equal(t, "bob", got)

	// Confirm keys do not collide: setting username does not overwrite user ID.
	ctx2 := WithUsername(ctx, "carol")
	require.Equal(t, "user-001", UserIDFromContext(ctx2))
	require.Equal(t, "req-002", RequestIDFromContext(ctx2))
	gotUsername, ok := ctx2.Value(UsernameKey).(string)
	require.True(t, ok)
	require.Equal(t, "carol", gotUsername)
}
