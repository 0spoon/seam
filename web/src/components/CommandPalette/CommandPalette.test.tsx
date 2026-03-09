import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

vi.mock('motion/react', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: ({ children, className, onClick, onKeyDown, role, ...props }: any) => {
      const safeProps: Record<string, unknown> = {};
      if (className) safeProps.className = className;
      if (onClick) safeProps.onClick = onClick;
      if (onKeyDown) safeProps.onKeyDown = onKeyDown;
      if (role) safeProps.role = role;
      if (props['aria-modal']) safeProps['aria-modal'] = props['aria-modal'];
      if (props['aria-label']) safeProps['aria-label'] = props['aria-label'];
      return <div {...safeProps}>{children}</div>;
    },
  },
}));

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('./CommandPalette.module.css', () => ({
  default: new Proxy({} as Record<string, string>, {
    get: (_target, prop: string) => prop,
  }),
}));

vi.mock('../../api/client', () => ({
  searchFTS: vi.fn().mockResolvedValue({ results: [], total: 0 }),
}));

vi.mock('../../lib/dates', () => ({
  timeAgo: () => 'just now',
}));

vi.mock('../../lib/navigation', () => ({
  navigate: vi.fn(),
  setNavigate: vi.fn(),
}));

vi.mock('../../lib/recentNotes', () => ({
  getRecentNotes: vi.fn(() => []),
  addRecentNote: vi.fn(),
  clearRecentNotes: vi.fn(),
}));

import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import { CommandPalette } from './CommandPalette';
import { getRecentNotes } from '../../lib/recentNotes';

vi.mock('../../stores/uiStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        commandPaletteOpen: false,
        setCommandPaletteOpen: vi.fn(),
        setCaptureModalOpen: vi.fn(),
        toggleSidebar: vi.fn(),
        toggleRightPanel: vi.fn(),
        setEditorViewMode: vi.fn(),
        setRightPanelOpen: vi.fn(),
        tags: [],
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn(() => ({
      setCommandPaletteOpen: vi.fn(),
      setCaptureModalOpen: vi.fn(),
      toggleSidebar: vi.fn(),
      toggleRightPanel: vi.fn(),
      setEditorViewMode: vi.fn(),
    })) },
  ),
}));

vi.mock('../../stores/projectStore', () => ({
  useProjectStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        projects: [],
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

function setUIState(overrides: Record<string, unknown>) {
  const defaults: Record<string, unknown> = {
    commandPaletteOpen: false,
    setCommandPaletteOpen: vi.fn(),
    setCaptureModalOpen: vi.fn(),
    toggleSidebar: vi.fn(),
    toggleRightPanel: vi.fn(),
    setEditorViewMode: vi.fn(),
    setRightPanelOpen: vi.fn(),
    tags: [],
  };
  const merged = { ...defaults, ...overrides };
  vi.mocked(useUIStore).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) => selector(merged),
  );
}

function setProjectState(overrides: Record<string, unknown>) {
  const defaults: Record<string, unknown> = {
    projects: [],
  };
  const merged = { ...defaults, ...overrides };
  vi.mocked(useProjectStore).mockImplementation(
    (selector: (s: Record<string, unknown>) => unknown) => selector(merged),
  );
}

function renderPalette() {
  return render(
    <MemoryRouter>
      <CommandPalette />
    </MemoryRouter>,
  );
}

describe('CommandPalette', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    setUIState({ commandPaletteOpen: false });
    setProjectState({ projects: [] });
  });

  it('renders nothing when closed', () => {
    const { container } = renderPalette();
    expect(container.innerHTML).toBe('');
  });

  it('renders command palette when open', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    expect(screen.getByRole('dialog')).toBeInTheDocument();
    expect(
      screen.getByPlaceholderText('Search notes, >commands, #tags, @projects...'),
    ).toBeInTheDocument();
  });

  it('shows "No recent notes" when empty query and no recents', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    expect(screen.getByText('No recent notes')).toBeInTheDocument();
  });

  it('shows recent notes when query is empty', () => {
    vi.mocked(getRecentNotes).mockReturnValue([
      { id: 'n1', title: 'My Note', openedAt: Date.now() },
    ]);
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    expect(screen.getByText('My Note')).toBeInTheDocument();
    expect(screen.getByText('RECENT')).toBeInTheDocument();
  });

  it('switches to command mode with > prefix', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '>' } });

    expect(screen.getByText('Commands')).toBeInTheDocument();
    expect(screen.getByText('COMMANDS')).toBeInTheDocument();
    // Should show default commands
    expect(screen.getByText('New note')).toBeInTheDocument();
    expect(screen.getByText('Search notes')).toBeInTheDocument();
    expect(screen.getByText('Toggle sidebar')).toBeInTheDocument();
  });

  it('filters commands by query in command mode', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '>graph' } });

    // Text is split across highlight spans, so use a function matcher.
    expect(
      screen.getByText((_content, el) =>
        el?.classList.contains('label') && el?.textContent === 'Graph view' || false,
      ),
    ).toBeInTheDocument();
    // Only one command result should appear
    const options = screen.getAllByRole('option');
    expect(options).toHaveLength(1);
  });

  it('shows tags in tag mode', () => {
    setUIState({
      commandPaletteOpen: true,
      tags: [
        { name: 'javascript', count: 5 },
        { name: 'rust', count: 3 },
      ],
    });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '#' } });

    expect(screen.getByText('Tags')).toBeInTheDocument();
    expect(screen.getByText('#javascript')).toBeInTheDocument();
    expect(screen.getByText('#rust')).toBeInTheDocument();
  });

  it('filters tags by query', () => {
    setUIState({
      commandPaletteOpen: true,
      tags: [
        { name: 'javascript', count: 5 },
        { name: 'rust', count: 3 },
      ],
    });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '#rust' } });

    // Text is split across highlight spans, so use a function matcher.
    expect(
      screen.getByText((_content, el) =>
        el?.classList.contains('label') && el?.textContent === '#rust' || false,
      ),
    ).toBeInTheDocument();
    // Only one tag result should appear
    const options = screen.getAllByRole('option');
    expect(options).toHaveLength(1);
  });

  it('shows projects in project mode', () => {
    setUIState({ commandPaletteOpen: true });
    setProjectState({
      projects: [
        { id: 'p1', name: 'Research' },
        { id: 'p2', name: 'Writing' },
      ],
    });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '@' } });

    expect(screen.getByText('Projects')).toBeInTheDocument();
    expect(screen.getByText('Research')).toBeInTheDocument();
    expect(screen.getByText('Writing')).toBeInTheDocument();
  });

  it('displays keyboard shortcuts for commands', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '>' } });

    expect(screen.getByText('Cmd+N')).toBeInTheDocument();
    expect(screen.getByText('/')).toBeInTheDocument();
  });

  it('closes on Escape', () => {
    const setOpen = vi.fn();
    setUIState({ commandPaletteOpen: true, setCommandPaletteOpen: setOpen });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.keyDown(input, { key: 'Escape' });

    expect(setOpen).toHaveBeenCalledWith(false);
  });

  it('shows "No results found" for non-matching command query', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText(
      'Search notes, >commands, #tags, @projects...',
    );
    fireEvent.change(input, { target: { value: '>xyznonexistent' } });

    expect(screen.getByText('No results found')).toBeInTheDocument();
  });
});
