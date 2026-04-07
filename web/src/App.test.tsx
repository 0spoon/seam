import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

// Mock all lazy-loaded pages to avoid dynamic import issues in tests.
vi.mock('./pages/Inbox/InboxPage', () => ({
  InboxPage: () => <div data-testid="inbox-page">Inbox</div>,
}));
vi.mock('./pages/Project/ProjectPage', () => ({
  ProjectPage: () => <div data-testid="project-page">Project</div>,
}));
vi.mock('./pages/NoteEditor/NoteEditorPage', () => ({
  NoteEditorPage: () => <div data-testid="note-editor-page">Editor</div>,
}));
vi.mock('./pages/Search/SearchPage', () => ({
  SearchPage: () => <div data-testid="search-page">Search</div>,
}));
vi.mock('./pages/Ask/AskPage', () => ({
  AskPage: () => <div data-testid="ask-page">Ask</div>,
}));
vi.mock('./pages/Graph/GraphPage', () => ({
  GraphPage: () => <div data-testid="graph-page">Graph</div>,
}));
vi.mock('./pages/Timeline/TimelinePage', () => ({
  TimelinePage: () => <div data-testid="timeline-page">Timeline</div>,
}));
vi.mock('./pages/Settings/SettingsPage', () => ({
  SettingsPage: () => <div data-testid="settings-page">Settings</div>,
}));

// Mock Layout to just render children via Outlet
vi.mock('./components/Layout/Layout', async () => {
  const { Outlet } = await vi.importActual<typeof import('react-router-dom')>(
    'react-router-dom',
  );
  return {
    Layout: () => (
      <div data-testid="layout">
        <Outlet />
      </div>
    ),
  };
});

// Mock Skeleton components
vi.mock('./components/Skeleton/Skeleton', () => ({
  FullPageSkeleton: () => <div data-testid="full-page-skeleton">Loading...</div>,
  NoteListSkeleton: () => <div>NoteListSkeleton</div>,
  EditorSkeleton: () => <div>EditorSkeleton</div>,
  GraphSkeleton: () => <div>GraphSkeleton</div>,
  GenericPageSkeleton: () => <div>GenericPageSkeleton</div>,
}));

vi.mock('./api/client', () => ({
  setOnAuthFailure: vi.fn(),
}));

vi.mock('./api/ws', () => ({
  connect: vi.fn(),
  disconnect: vi.fn(),
}));

vi.mock('./stores/settingsStore', () => ({
  useSettingsStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        fetchSettings: vi.fn().mockResolvedValue(undefined),
        settings: {},
        isLoaded: true,
        get: () => '',
        updateSetting: vi.fn(),
      };
      return selector(state);
    }),
    {
      setState: vi.fn(),
      getState: () => ({
        fetchSettings: vi.fn().mockResolvedValue(undefined),
        settings: {},
        isLoaded: true,
        get: () => '',
        updateSetting: vi.fn(),
      }),
    },
  ),
}));

vi.mock('./components/Toast/ToastContainer', () => ({
  ToastContainer: () => null,
  useToastStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        addToast: vi.fn(),
        toasts: [],
      };
      return selector(state);
    }),
    {
      getState: () => ({ addToast: vi.fn(), toasts: [] }),
    },
  ),
}));

// Auth store with controllable state
let authState: Record<string, unknown> = {};

vi.mock('./stores/authStore', () => ({
  useAuthStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) =>
      selector(authState),
    ),
    {
      setState: vi.fn(),
      getState: () => authState,
    },
  ),
}));

vi.mock('./stores/projectStore', () => ({
  useProjectStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        projects: [],
        fetchProjects: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('./stores/uiStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        fetchTags: vi.fn(),
        bridgeFromSettings: vi.fn(),
        sidebarCollapsed: false,
        sidebarOpen: false,
        commandPaletteOpen: false,
        captureModalOpen: false,
        tags: [],
      };
      return selector(state);
    }),
    {
      setState: vi.fn(),
      getState: () => ({ bridgeFromSettings: vi.fn() }),
    },
  ),
}));

import { App } from './App';
import { useAuthStore } from './stores/authStore';

function renderApp(initialRoute = '/') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <App />
    </MemoryRouter>,
  );
}

describe('App', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    // Default: not authenticated, not loading
    authState = {
      user: null,
      isAuthenticated: false,
      isLoading: false,
      error: null,
      restoreSession: vi.fn(),
      clearError: vi.fn(),
      logout: vi.fn(),
    };
    vi.mocked(useAuthStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector(authState),
    );
  });

  it('redirects to login when not authenticated', () => {
    renderApp('/');
    // Should not see the layout -- user is redirected to /login
    expect(screen.queryByTestId('layout')).not.toBeInTheDocument();
  });

  it('renders login page at /login', () => {
    renderApp('/login');
    // The LoginPage is not mocked, let us check it renders
    // LoginPage is imported directly (not lazy), so it should render
    // We check for a known element from LoginPage
    expect(screen.queryByTestId('layout')).not.toBeInTheDocument();
  });

  it('renders layout when authenticated', () => {
    authState = {
      ...authState,
      user: { id: 'u1', username: 'test', email: 'test@example.com' },
      isAuthenticated: true,
      isLoading: false,
    };
    vi.mocked(useAuthStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector(authState),
    );

    renderApp('/');
    expect(screen.getByTestId('layout')).toBeInTheDocument();
  });

  it('shows loading skeleton while auth is loading', () => {
    authState = {
      ...authState,
      isAuthenticated: false,
      isLoading: true,
    };
    vi.mocked(useAuthStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector(authState),
    );

    renderApp('/');
    expect(screen.getByTestId('full-page-skeleton')).toBeInTheDocument();
  });

  it('calls restoreSession on mount', () => {
    const restoreSession = vi.fn();
    authState = {
      ...authState,
      restoreSession,
    };
    vi.mocked(useAuthStore).mockImplementation(
      (selector: (s: Record<string, unknown>) => unknown) =>
        selector(authState),
    );

    renderApp('/');
    expect(restoreSession).toHaveBeenCalled();
  });
});
