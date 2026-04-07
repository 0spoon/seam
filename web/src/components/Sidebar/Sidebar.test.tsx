import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

vi.mock('../../api/client', () => ({
  searchFTS: vi.fn().mockResolvedValue({ results: [] }),
}));

vi.mock('../../hooks/useKeyboard', () => ({
  useKeyboard: vi.fn(),
}));

vi.mock('../../hooks/useWebSocket', () => ({
  useNoteWebSocket: vi.fn(),
}));

vi.mock('../../lib/sanitize', () => ({
  sanitizeHtml: (s: string) => s,
}));

vi.mock('../../lib/tagColor', () => ({
  getProjectColor: (i: number) => `hsl(${i * 60}, 70%, 50%)`,
}));

vi.mock('../ConfirmModal/ConfirmModal', () => ({
  ConfirmModal: () => null,
}));

vi.mock('./Sidebar.module.css', () => ({
  default: new Proxy({} as Record<string, string>, {
    get: (_target, prop: string) => prop,
  }),
}));

vi.mock('motion/react', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: ({
      children,
      className,
    }: {
      children?: React.ReactNode;
      className?: string;
    }) => {
      const safeProps: Record<string, unknown> = {};
      if (className) safeProps.className = className;
      return <div {...safeProps}>{children}</div>;
    },
  },
}));

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('../../stores/uiStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        sidebarCollapsed: false,
        sidebarOpen: false,
        setSidebarOpen: vi.fn(),
        toggleSidebar: vi.fn(),
        tags: [
          { name: 'go', count: 5 },
          { name: 'rust', count: 3 },
        ],
        setCaptureModalOpen: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('../../stores/projectStore', () => ({
  useProjectStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        projects: [
          { id: 'p1', name: 'Alpha Project' },
          { id: 'p2', name: 'Beta Project' },
        ],
        createProject: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('../../stores/settingsStore', () => ({
  useSettingsStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        settings: {
          sidebar_projects_expanded: 'true',
          sidebar_tags_expanded: 'true',
        },
        updateSetting: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('../../stores/authStore', () => ({
  useAuthStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        user: { id: 'u1', username: 'testuser', email: 'test@example.com' },
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('../../stores/noteStore', () => ({
  useNoteStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        createNote: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

vi.mock('../Toast/ToastContainer', () => ({
  useToastStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        addToast: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

import { Sidebar } from './Sidebar';

function renderSidebar() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Sidebar />
    </MemoryRouter>,
  );
}

describe('Sidebar', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders Inbox navigation item', () => {
    renderSidebar();
    expect(screen.getByTitle('Inbox')).toBeInTheDocument();
  });

  it('renders Ask Seam navigation item', () => {
    renderSidebar();
    expect(screen.getByTitle('Ask Seam')).toBeInTheDocument();
  });

  it('renders Graph navigation item', () => {
    renderSidebar();
    expect(screen.getByTitle('Knowledge Graph')).toBeInTheDocument();
  });

  it('renders Timeline navigation item', () => {
    renderSidebar();
    expect(screen.getByTitle('Timeline')).toBeInTheDocument();
  });

  it('renders collapse toggle button', () => {
    renderSidebar();
    expect(screen.getByLabelText('Collapse sidebar')).toBeInTheDocument();
  });

  it('renders Capture button', () => {
    renderSidebar();
    expect(screen.getByLabelText('Quick capture')).toBeInTheDocument();
  });

  it('renders project list', () => {
    renderSidebar();
    expect(screen.getByTitle('Alpha Project')).toBeInTheDocument();
    expect(screen.getByTitle('Beta Project')).toBeInTheDocument();
  });

  it('renders Projects section header', () => {
    renderSidebar();
    expect(screen.getByText('Projects')).toBeInTheDocument();
  });

  it('renders Tags section header', () => {
    renderSidebar();
    expect(screen.getByText('Tags')).toBeInTheDocument();
  });

  it('renders tag list', () => {
    renderSidebar();
    expect(screen.getByText('#go')).toBeInTheDocument();
    expect(screen.getByText('#rust')).toBeInTheDocument();
  });

  it('renders user avatar with first letter', () => {
    renderSidebar();
    expect(screen.getByText('T')).toBeInTheDocument();
  });

  it('renders username', () => {
    renderSidebar();
    expect(screen.getByText('testuser')).toBeInTheDocument();
  });

  it('renders settings button', () => {
    renderSidebar();
    expect(screen.getByLabelText('Settings')).toBeInTheDocument();
  });

  it('renders create project button', () => {
    renderSidebar();
    expect(screen.getByLabelText('Create project')).toBeInTheDocument();
  });

  it('renders navigation landmark', () => {
    renderSidebar();
    expect(screen.getByRole('navigation')).toBeInTheDocument();
  });
});
