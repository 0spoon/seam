import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { ProjectPage } from './ProjectPage';
import { useNoteStore } from '../../stores/noteStore';
import { useProjectStore } from '../../stores/projectStore';
import { useUIStore } from '../../stores/uiStore';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return {
    ...actual,
    useNavigate: () => mockNavigate,
    useParams: () => ({ id: 'proj1' }),
  };
});

vi.mock('@tanstack/react-virtual', () => ({
  useVirtualizer: () => ({
    getVirtualItems: () => [],
    getTotalSize: () => 0,
    measureElement: vi.fn(),
  }),
}));

vi.mock('motion/react', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: ({
      children,
      ...props
    }: {
      children?: React.ReactNode;
      [key: string]: unknown;
    }) => {
      const safe = Object.fromEntries(
        Object.entries(props).filter(
          ([k]) =>
            !['initial', 'animate', 'exit', 'transition', 'layout', 'ref', 'data-index'].includes(k),
        ),
      );
      return <div {...safe}>{children}</div>;
    },
  },
}));

function renderProjectPage() {
  return render(
    <MemoryRouter>
      <ProjectPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
  useProjectStore.setState({
    currentProject: {
      id: 'proj1',
      name: 'Test Project',
      slug: 'test-project',
      description: '',
      created_at: '2025-01-01T00:00:00Z',
      updated_at: '2025-01-01T00:00:00Z',
    },
    projects: [
      {
        id: 'proj1',
        name: 'Test Project',
        slug: 'test-project',
        description: '',
        created_at: '2025-01-01T00:00:00Z',
        updated_at: '2025-01-01T00:00:00Z',
      },
    ],
    isLoading: false,
    error: null,
    fetchProject: vi.fn().mockResolvedValue(undefined),
  });
  useNoteStore.setState({
    notes: [],
    total: 0,
    isLoading: false,
    error: null,
    fetchNotes: vi.fn().mockResolvedValue(undefined),
  });
  useUIStore.setState({
    captureModalOpen: false,
    captureDefaultProjectId: '',
  });
});

describe('ProjectPage', () => {
  it('renders project name in header', () => {
    renderProjectPage();
    expect(screen.getByText('Test Project')).toBeInTheDocument();
  });

  it('shows empty state when no notes', () => {
    renderProjectPage();
    expect(screen.getByText('No notes yet')).toBeInTheDocument();
    expect(
      screen.getByText('Create the first note in this project'),
    ).toBeInTheDocument();
  });

  it('renders "New note" button', () => {
    renderProjectPage();
    const buttons = screen.getAllByText('New note');
    expect(buttons.length).toBeGreaterThanOrEqual(1);
  });
});
