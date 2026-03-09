import { useEffect } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuthStore } from './stores/authStore';
import { useProjectStore } from './stores/projectStore';
import { useUIStore } from './stores/uiStore';
import { setOnAuthFailure } from './api/client';
import { Layout } from './components/Layout/Layout';
import { LoginPage } from './pages/Login/LoginPage';
import { InboxPage } from './pages/Inbox/InboxPage';
import { ProjectPage } from './pages/Project/ProjectPage';
import { NoteEditorPage } from './pages/NoteEditor/NoteEditorPage';
import { SearchPage } from './pages/Search/SearchPage';
import { AskPage } from './pages/Ask/AskPage';
import { GraphPage } from './pages/Graph/GraphPage';
import { TimelinePage } from './pages/Timeline/TimelinePage';

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isLoading = useAuthStore((s) => s.isLoading);

  if (isLoading) {
    return <div style={{ padding: '2rem', color: 'var(--text-tertiary)' }}>Loading...</div>;
  }

  if (!isAuthenticated) {
    return <Navigate to="/login" replace />;
  }

  return <>{children}</>;
}

export function App() {
  const restoreSession = useAuthStore((s) => s.restoreSession);
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const fetchProjects = useProjectStore((s) => s.fetchProjects);
  const fetchTags = useUIStore((s) => s.fetchTags);

  useEffect(() => {
    restoreSession();
    setOnAuthFailure(() => {
      useAuthStore.getState().logout();
    });
  }, [restoreSession]);

  useEffect(() => {
    if (isAuthenticated) {
      fetchProjects();
      fetchTags();
    }
  }, [isAuthenticated, fetchProjects, fetchTags]);

  return (
    <Routes>
      <Route path="/login" element={<LoginPage />} />
      <Route
        element={
          <ProtectedRoute>
            <Layout />
          </ProtectedRoute>
        }
      >
        <Route index element={<InboxPage />} />
        <Route path="projects/:id" element={<ProjectPage />} />
        <Route path="notes/:id" element={<NoteEditorPage />} />
        <Route path="search" element={<SearchPage />} />
        <Route path="ask" element={<AskPage />} />
        <Route path="graph" element={<GraphPage />} />
        <Route path="timeline" element={<TimelinePage />} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
