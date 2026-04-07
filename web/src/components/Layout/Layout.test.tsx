import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';

vi.mock('../Sidebar/Sidebar', () => ({
  Sidebar: () => <div data-testid="sidebar">Sidebar</div>,
}));

vi.mock('../CommandPalette/CommandPalette', () => ({
  CommandPalette: () => <div data-testid="command-palette" />,
}));

vi.mock('../Modal/CaptureModal', () => ({
  CaptureModal: () => <div data-testid="capture-modal" />,
}));

vi.mock('../Toast/ToastContainer', () => ({
  ToastContainer: () => <div data-testid="toast-container" />,
  useToastStore: Object.assign(vi.fn(), {
    getState: () => ({ addToast: vi.fn() }),
  }),
}));

vi.mock('../../hooks/useKeyboard', () => ({
  useKeyboard: vi.fn(),
}));

vi.mock('motion/react', () => ({
  AnimatePresence: ({ children }: { children: React.ReactNode }) => <>{children}</>,
  motion: {
    div: ({
      children,
      className,
      ...props
    }: {
      children?: React.ReactNode;
      className?: string;
      [key: string]: unknown;
    }) => {
      const safeProps: Record<string, unknown> = {};
      if (className) safeProps.className = className;
      // Filter out motion-specific props
      for (const [k, v] of Object.entries(props)) {
        if (!['initial', 'animate', 'exit', 'transition', 'layout'].includes(k)) {
          safeProps[k] = v;
        }
      }
      return <div {...safeProps}>{children}</div>;
    },
  },
}));

vi.mock('./Layout.module.css', () => ({
  default: new Proxy({} as Record<string, string>, {
    get: (_target, prop: string) => prop,
  }),
}));

vi.mock('../../stores/uiStore', () => ({
  useUIStore: Object.assign(
    vi.fn((selector: (s: Record<string, unknown>) => unknown) => {
      const state: Record<string, unknown> = {
        sidebarCollapsed: false,
        sidebarOpen: false,
        setSidebarOpen: vi.fn(),
        setCommandPaletteOpen: vi.fn(),
        setCaptureModalOpen: vi.fn(),
        toggleSidebar: vi.fn(),
      };
      return selector(state);
    }),
    { setState: vi.fn(), getState: vi.fn() },
  ),
}));

import { Layout } from './Layout';

function renderLayout() {
  return render(
    <MemoryRouter initialEntries={['/']}>
      <Layout />
    </MemoryRouter>,
  );
}

describe('Layout', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders sidebar', () => {
    renderLayout();
    expect(screen.getByTestId('sidebar')).toBeInTheDocument();
  });

  it('renders command palette', () => {
    renderLayout();
    expect(screen.getByTestId('command-palette')).toBeInTheDocument();
  });

  it('renders capture modal', () => {
    renderLayout();
    expect(screen.getByTestId('capture-modal')).toBeInTheDocument();
  });

  it('renders toast container', () => {
    renderLayout();
    expect(screen.getByTestId('toast-container')).toBeInTheDocument();
  });

  it('renders main content area', () => {
    renderLayout();
    const main = screen.getByRole('main');
    expect(main).toBeInTheDocument();
    expect(main.id).toBe('main-content');
  });

  it('renders hamburger button for mobile', () => {
    renderLayout();
    const button = screen.getByLabelText('Open navigation');
    expect(button).toBeInTheDocument();
  });

  it('renders skip-to-content link', () => {
    renderLayout();
    expect(screen.getByText('Skip to content')).toBeInTheDocument();
  });

  it('renders screen reader route announcer', () => {
    renderLayout();
    const announcer = document.querySelector('[aria-live="assertive"]');
    expect(announcer).toBeInTheDocument();
    expect(announcer?.textContent).toContain('Navigated to');
  });
});
