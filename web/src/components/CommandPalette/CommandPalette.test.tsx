import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent } from '@testing-library/react';
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

import { useUIStore } from '../../stores/uiStore';
import { useProjectStore } from '../../stores/projectStore';
import { CommandPalette } from './CommandPalette';

vi.mock('../../stores/uiStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        commandPaletteOpen: false,
        setCommandPaletteOpen: vi.fn(),
        setCaptureModalOpen: vi.fn(),
        toggleSidebar: vi.fn(),
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
    expect(screen.getByPlaceholderText('Type a command...')).toBeInTheDocument();
  });

  it('shows base command items', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    expect(screen.getByText('New note')).toBeInTheDocument();
    expect(screen.getByText('Search notes')).toBeInTheDocument();
    expect(screen.getByText('Graph view')).toBeInTheDocument();
    expect(screen.getByText('Timeline')).toBeInTheDocument();
    expect(screen.getByText('Ask Seam')).toBeInTheDocument();
    expect(screen.getByText('Toggle sidebar')).toBeInTheDocument();
  });

  it('shows project commands when projects exist', () => {
    setUIState({ commandPaletteOpen: true });
    setProjectState({
      projects: [
        { id: 'p1', name: 'Research' },
        { id: 'p2', name: 'Writing' },
      ],
    });
    renderPalette();

    expect(screen.getByText('Open project: Research')).toBeInTheDocument();
    expect(screen.getByText('Open project: Writing')).toBeInTheDocument();
  });

  it('filters commands by query', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText('Type a command...');
    fireEvent.change(input, { target: { value: 'graph' } });

    expect(screen.getByText('Graph view')).toBeInTheDocument();
    expect(screen.queryByText('New note')).not.toBeInTheDocument();
    expect(screen.queryByText('Search notes')).not.toBeInTheDocument();
  });

  it('shows "No matching commands" for non-matching query', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const input = screen.getByPlaceholderText('Type a command...');
    fireEvent.change(input, { target: { value: 'xyznonexistent' } });

    expect(screen.getByText('No matching commands')).toBeInTheDocument();
  });

  it('displays keyboard shortcuts for commands that have them', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    expect(screen.getByText('Cmd+N')).toBeInTheDocument();
    expect(screen.getByText('/')).toBeInTheDocument();
  });

  it('renders all command items as role=option', () => {
    setUIState({ commandPaletteOpen: true });
    renderPalette();

    const options = screen.getAllByRole('option');
    // 6 base commands, no projects
    expect(options.length).toBe(6);
  });
});
