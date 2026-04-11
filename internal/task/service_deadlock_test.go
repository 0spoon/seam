package task

import (
	"context"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/userdb"
)

func TestService_ToggleDone_NoDeadlock(t *testing.T) {
	dataDir := t.TempDir()
	logger := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelError}))
	mgr := userdb.NewSQLManager(dataDir, logger)
	t.Cleanup(func() { mgr.CloseAll() })

	noteSvc := note.NewService(
		note.NewSQLStore(),
		note.NewVersionStore(),
		project.NewStore(),
		mgr,
		nil,
		logger,
	)

	taskSvc := NewService(NewStore(), mgr, logger)
	taskSvc.SetNoteService(noteSvc)

	ctx := context.Background()
	const userID = "test-user-001"

	n, err := noteSvc.Create(ctx, userID, note.CreateNoteReq{
		Title: "With Checkbox",
		Body:  "- [ ] buy milk\n- [ ] walk dog\n",
	})
	require.NoError(t, err)

	require.NoError(t, taskSvc.SyncNote(ctx, userID, n.ID, n.Body))

	tasks, _, err := taskSvc.List(ctx, userID, TaskFilter{NoteID: n.ID, Limit: 10})
	require.NoError(t, err)
	require.Len(t, tasks, 2)

	done := make(chan error, 1)
	go func() {
		done <- taskSvc.ToggleDone(ctx, userID, tasks[0].ID, true)
	}()

	select {
	case err := <-done:
		require.NoError(t, err)
	case <-time.After(5 * time.Second):
		t.Fatal("ToggleDone deadlocked: 5s timeout")
	}
}
