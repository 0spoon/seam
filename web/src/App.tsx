import { useEffect, lazy, Suspense } from 'react';
import { Routes, Route, Navigate } from 'react-router-dom';
import { useAuthStore } from './stores/authStore';
import { useProjectStore } from './stores/projectStore';
import { useUIStore } from './stores/uiStore';
import { useSettingsStore } from './stores/settingsStore';
import { setOnAuthFailure } from './api/client';
import { Layout } from './components/Layout/Layout';
import { LoginPage } from './pages/Login/LoginPage';
import {
  FullPageSkeleton,
  NoteListSkeleton,
  EditorSkeleton,
  GraphSkeleton,
  GenericPageSkeleton,
} from './components/Skeleton/Skeleton';

// Lazy-loaded page components for code splitting.
const InboxPage = lazy(() => import('./pages/Inbox/InboxPage').then((m) => ({ default: m.InboxPage })));
const ProjectPage = lazy(() => import('./pages/Project/ProjectPage').then((m) => ({ default: m.ProjectPage })));
const NoteEditorPage = lazy(() => import('./pages/NoteEditor/NoteEditorPage').then((m) => ({ default: m.NoteEditorPage })));
const SearchPage = lazy(() => import('./pages/Search/SearchPage').then((m) => ({ default: m.SearchPage })));
const AskPage = lazy(() => import('./pages/Ask/AskPage').then((m) => ({ default: m.AskPage })));
const GraphPage = lazy(() => import('./pages/Graph/GraphPage').then((m) => ({ default: m.GraphPage })));
const TimelinePage = lazy(() => import('./pages/Timeline/TimelinePage').then((m) => ({ default: m.TimelinePage })));
const SettingsPage = lazy(() => import('./pages/Settings/SettingsPage').then((m) => ({ default: m.SettingsPage })));

function NoteListFallback() {
  return (
    <div style={{ padding: 'var(--space-6)' }}>
      <NoteListSkeleton count={4} />
    </div>
  );
}

function EditorFallback() {
  return (
    <div style={{ padding: 'var(--space-6)' }}>
      <EditorSkeleton />
    </div>
  );
}

function GraphFallback() {
  return <GraphSkeleton />;
}

function GenericFallback() {
  return <GenericPageSkeleton />;
}

function ProtectedRoute({ children }: { children: React.ReactNode }) {
  const isAuthenticated = useAuthStore((s) => s.isAuthenticated);
  const isLoading = useAuthStore((s) => s.isLoading);

  if (isLoading) {
    return <FullPageSkeleton />;
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
  const fetchSettings = useSettingsStore((s) => s.fetchSettings);

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
      fetchSettings().then(() => {
        useUIStore.getState().bridgeFromSettings();
      });
    }
  }, [isAuthenticated, fetchProjects, fetchTags, fetchSettings]);

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
        <Route index element={<Suspense fallback={<NoteListFallback />}><InboxPage /></Suspense>} />
        <Route path="projects/:id" element={<Suspense fallback={<NoteListFallback />}><ProjectPage /></Suspense>} />
        <Route path="notes/:id" element={<Suspense fallback={<EditorFallback />}><NoteEditorPage /></Suspense>} />
        <Route path="search" element={<Suspense fallback={<NoteListFallback />}><SearchPage /></Suspense>} />
        <Route path="ask" element={<Suspense fallback={<GenericFallback />}><AskPage /></Suspense>} />
        <Route path="graph" element={<Suspense fallback={<GraphFallback />}><GraphPage /></Suspense>} />
        <Route path="timeline" element={<Suspense fallback={<NoteListFallback />}><TimelinePage /></Suspense>} />
        <Route path="settings" element={<Suspense fallback={<GenericFallback />}><SettingsPage /></Suspense>} />
      </Route>
      <Route path="*" element={<Navigate to="/" replace />} />
    </Routes>
  );
}
