package assistant

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestToolRegistry_Register_And_Get(t *testing.T) {
	registry := NewToolRegistry()

	tool := &Tool{
		Name:        "test_tool",
		Description: "A test tool",
		Parameters:  json.RawMessage(`{"type":"object"}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return json.RawMessage(`{"ok":true}`), nil
		},
		ReadOnly: true,
	}

	registry.Register(tool)

	got, err := registry.Get("test_tool")
	require.NoError(t, err)
	require.Equal(t, "test_tool", got.Name)
	require.Equal(t, "A test tool", got.Description)
	require.True(t, got.ReadOnly)
}

func TestToolRegistry_Get_NotFound(t *testing.T) {
	registry := NewToolRegistry()

	_, err := registry.Get("nonexistent")
	require.ErrorIs(t, err, ErrToolNotFound)
}

func TestToolRegistry_Definitions(t *testing.T) {
	registry := NewToolRegistry()

	registry.Register(&Tool{
		Name:        "tool_a",
		Description: "Tool A",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return nil, nil
		},
	})
	registry.Register(&Tool{
		Name:        "tool_b",
		Description: "Tool B",
		Parameters:  json.RawMessage(`{"type":"object","properties":{}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			return nil, nil
		},
	})

	defs := registry.Definitions()
	require.Len(t, defs, 2)

	names := registry.Names()
	require.Len(t, names, 2)
	require.ElementsMatch(t, []string{"tool_a", "tool_b"}, names)
}

func TestToolRegistry_Execute(t *testing.T) {
	registry := NewToolRegistry()

	registry.Register(&Tool{
		Name:        "echo",
		Description: "Echoes input",
		Parameters:  json.RawMessage(`{"type":"object","properties":{"msg":{"type":"string"}}}`),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Msg string `json:"msg"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, err
			}
			return json.RawMessage(`{"echo":"` + params.Msg + `"}`), nil
		},
		ReadOnly: true,
	})

	tool, err := registry.Get("echo")
	require.NoError(t, err)

	result, err := tool.Func(context.Background(), "user1", json.RawMessage(`{"msg":"hello"}`))
	require.NoError(t, err)

	var resp struct {
		Echo string `json:"echo"`
	}
	require.NoError(t, json.Unmarshal(result, &resp))
	require.Equal(t, "hello", resp.Echo)
}
