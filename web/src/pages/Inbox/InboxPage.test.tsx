import React from 'react';
import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { InboxPage } from './InboxPage';
import { useNoteStore } from '../../stores/noteStore';
import { useUIStore } from '../../stores/uiStore';

const mockNavigate = vi.fn();
vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
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
    div: ({ children, ...props }: { children?: React.ReactNode; [key: string]: unknown }) => {
      const safe = Object.fromEntries(
        Object.entries(props).filter(
          ([k]) =>
            !['initial', 'animate', 'exit', 'transition', 'layout', 'ref', 'data-index'].includes(
              k,
            ),
        ),
      );
      return <div {...safe}>{children}</div>;
    },
  },
}));

function renderInboxPage(initialRoute = '/') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <InboxPage />
    </MemoryRouter>,
  );
}

beforeEach(() => {
  vi.clearAllMocks();
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
    tags: [],
    fetchTags: vi.fn().mockResolvedValue(undefined),
  });
});

describe('InboxPage', () => {
  it('renders "Inbox" title', () => {
    renderInboxPage();
    expect(screen.getByText('Inbox')).toBeInTheDocument();
  });

  it('shows empty state when no notes', () => {
    renderInboxPage();
    expect(screen.getByText('Nothing in the inbox')).toBeInTheDocument();
    expect(screen.getByText('Capture a thought to get started')).toBeInTheDocument();
  });

  it('shows tag filter pill when tag query param present', () => {
    renderInboxPage('/?tag=design');
    expect(screen.getByText('#design')).toBeInTheDocument();
  });
});
