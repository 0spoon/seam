package ws

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/coder/websocket"
	"github.com/stretchr/testify/require"
)

// setupTestServer creates an httptest.Server that upgrades connections to
// WebSocket, registers them with the hub under the user ID provided in
// the "user" query parameter, and keeps the connection alive until the
// test server shuts down.
func setupTestServer(t *testing.T, hub *Hub) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := websocket.Accept(w, r, nil)
		if err != nil {
			return
		}
		userID := r.URL.Query().Get("user")
		hub.Register(userID, conn)
		defer hub.Unregister(userID, conn)
		// Read loop keeps the connection alive and detects client disconnect.
		for {
			_, _, readErr := conn.Read(r.Context())
			if readErr != nil {
				return
			}
		}
	}))
	t.Cleanup(srv.Close)
	return srv
}

// dialWS dials the test server and returns a client-side websocket.Conn.
func dialWS(t *testing.T, srv *httptest.Server, userID string) *websocket.Conn {
	t.Helper()
	url := "ws" + strings.TrimPrefix(srv.URL, "http") + "?user=" + userID
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, _, err := websocket.Dial(ctx, url, nil)
	require.NoError(t, err)
	t.Cleanup(func() { conn.CloseNow() })
	return conn
}

func testLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(nil, &slog.HandlerOptions{Level: slog.LevelError}))
}

func TestHub_NewHub(t *testing.T) {
	h := NewHub(nil)
	require.NotNil(t, h)
	require.Equal(t, 0, h.ConnectionCount())
}

func TestHub_RegisterUnregister(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	// Register two connections for user "alice".
	_ = dialWS(t, srv, "alice")
	_ = dialWS(t, srv, "alice")

	// Give the server goroutines a moment to register.
	requireEventually(t, func() bool { return hub.ConnectionCount() == 2 }, 2*time.Second)

	// Register one connection for user "bob".
	_ = dialWS(t, srv, "bob")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 3 }, 2*time.Second)
}

func TestHub_ConnectionCount_AfterUnregister(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	conn := dialWS(t, srv, "carol")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 1 }, 2*time.Second)

	// Close the client-side connection. The server handler's context will
	// eventually end, triggering the defer Unregister. But we can also
	// manually close and check that the hub cleans up.
	conn.Close(websocket.StatusNormalClosure, "bye")

	// The server side will unregister when the handler returns after detecting
	// the client closed. Wait for that.
	requireEventually(t, func() bool { return hub.ConnectionCount() == 0 }, 2*time.Second)
}

func TestHub_Send_ToSpecificUser(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	aliceConn := dialWS(t, srv, "alice")
	bobConn := dialWS(t, srv, "bob")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 2 }, 2*time.Second)

	// Send a message to alice only.
	payload, err := json.Marshal(map[string]string{"note_id": "123"})
	require.NoError(t, err)
	msg := Message{Type: MsgTypeNoteChanged, Payload: payload}
	err = hub.Send("alice", msg)
	require.NoError(t, err)

	// Alice should receive the message.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	_, data, err := aliceConn.Read(ctx)
	require.NoError(t, err)

	var received Message
	err = json.Unmarshal(data, &received)
	require.NoError(t, err)
	require.Equal(t, MsgTypeNoteChanged, received.Type)

	// Bob should NOT receive anything within a short window.
	bobCtx, bobCancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer bobCancel()
	_, _, err = bobConn.Read(bobCtx)
	require.Error(t, err, "bob should not receive a message sent only to alice")
}

func TestHub_Send_MultipleConnectionsSameUser(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	conn1 := dialWS(t, srv, "dave")
	conn2 := dialWS(t, srv, "dave")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 2 }, 2*time.Second)

	payload, err := json.Marshal(map[string]string{"status": "ok"})
	require.NoError(t, err)
	msg := Message{Type: MsgTypeTaskComplete, Payload: payload}
	err = hub.Send("dave", msg)
	require.NoError(t, err)

	// Both connections should receive the message.
	for _, conn := range []*websocket.Conn{conn1, conn2} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, data, readErr := conn.Read(ctx)
		cancel()
		require.NoError(t, readErr)

		var received Message
		require.NoError(t, json.Unmarshal(data, &received))
		require.Equal(t, MsgTypeTaskComplete, received.Type)
	}
}

func TestHub_Broadcast(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	aliceConn := dialWS(t, srv, "alice")
	bobConn := dialWS(t, srv, "bob")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 2 }, 2*time.Second)

	payload, err := json.Marshal(map[string]string{"info": "system"})
	require.NoError(t, err)
	msg := Message{Type: MsgTypeTaskProgress, Payload: payload}
	err = hub.Broadcast(msg)
	require.NoError(t, err)

	// Both users should receive the broadcast.
	for _, conn := range []*websocket.Conn{aliceConn, bobConn} {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		_, data, readErr := conn.Read(ctx)
		cancel()
		require.NoError(t, readErr)

		var received Message
		require.NoError(t, json.Unmarshal(data, &received))
		require.Equal(t, MsgTypeTaskProgress, received.Type)
	}
}

func TestHub_Send_NoUser_NoError(t *testing.T) {
	hub := NewHub(testLogger())
	payload, _ := json.Marshal(map[string]string{"x": "y"})
	err := hub.Send("nobody", Message{Type: "test", Payload: payload})
	require.NoError(t, err, "sending to a non-existent user should not error")
}

func TestHub_DeadConnectionCleanup(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	conn := dialWS(t, srv, "eve")
	requireEventually(t, func() bool { return hub.ConnectionCount() == 1 }, 2*time.Second)

	// Close the client side abruptly so that the server-side write will fail.
	conn.CloseNow()

	// Give the server handler a moment to notice the close and unregister.
	// If that is not enough, Send will clean up the dead connection.
	time.Sleep(100 * time.Millisecond)

	payload, _ := json.Marshal(map[string]string{"ping": "1"})
	msg := Message{Type: "test", Payload: payload}
	err := hub.Send("eve", msg)
	require.NoError(t, err)

	// After sending, the dead connection should be cleaned up.
	requireEventually(t, func() bool { return hub.ConnectionCount() == 0 }, 2*time.Second)
}

func TestHub_ConcurrentRegisterUnregister(t *testing.T) {
	hub := NewHub(testLogger())
	srv := setupTestServer(t, hub)

	const n = 10
	var wg sync.WaitGroup
	wg.Add(n)
	for i := 0; i < n; i++ {
		go func() {
			defer wg.Done()
			c := dialWS(t, srv, "concurrent")
			time.Sleep(50 * time.Millisecond)
			c.Close(websocket.StatusNormalClosure, "done")
		}()
	}
	wg.Wait()

	// All connections should eventually be cleaned up.
	requireEventually(t, func() bool { return hub.ConnectionCount() == 0 }, 5*time.Second)
}

// requireEventually polls fn until it returns true or the timeout expires.
func requireEventually(t *testing.T, fn func() bool, timeout time.Duration) {
	t.Helper()
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatal("condition not met within timeout")
}
