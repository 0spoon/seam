package ai

import (
	"context"
	"database/sql"
	"fmt"
	"sync"

	_ "modernc.org/sqlite"

	"github.com/katata/seam/migrations"
)

// mockDBManager implements userdb.Manager for tests.
// It uses in-memory SQLite databases per user, with a unique prefix
// to ensure test isolation across parallel tests.
type mockDBManager struct {
	mu     sync.Mutex
	dbs    map[string]*sql.DB
	prefix string
}

var mockCounter uint64
var mockCounterMu sync.Mutex

func newMockDBManager() *mockDBManager {
	mockCounterMu.Lock()
	mockCounter++
	id := mockCounter
	mockCounterMu.Unlock()
	return &mockDBManager{
		dbs:    make(map[string]*sql.DB),
		prefix: fmt.Sprintf("mock_%d", id),
	}
}

func (m *mockDBManager) Open(_ context.Context, userID string) (*sql.DB, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	if m.dbs == nil {
		m.dbs = make(map[string]*sql.DB)
	}
	if db, ok := m.dbs[userID]; ok {
		return db, nil
	}

	name := fmt.Sprintf("file:%s_%s?mode=memory&cache=shared", m.prefix, userID)
	db, err := sql.Open("sqlite", name)
	if err != nil {
		return nil, err
	}
	db.Exec("PRAGMA journal_mode=WAL")
	db.Exec("PRAGMA foreign_keys=ON")
	db.Exec(migrations.UserSQL)

	m.dbs[userID] = db
	return db, nil
}

func (m *mockDBManager) Close(userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if db, ok := m.dbs[userID]; ok {
		db.Close()
		delete(m.dbs, userID)
	}
	return nil
}

func (m *mockDBManager) CloseAll() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, db := range m.dbs {
		db.Close()
	}
	m.dbs = nil
	return nil
}

func (m *mockDBManager) UserNotesDir(userID string) string {
	return "/tmp/test-notes/" + userID
}

func (m *mockDBManager) UserDataDir(userID string) string {
	return "/tmp/test-data/" + userID
}

func (m *mockDBManager) ListUsers(_ context.Context) ([]string, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	var users []string
	for uid := range m.dbs {
		users = append(users, uid)
	}
	return users, nil
}

func (m *mockDBManager) EnsureUserDirs(userID string) error {
	return nil
}
