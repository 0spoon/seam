import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { TimelinePage } from './TimelinePage';

// Mock the API client.
vi.mock('../../api/client', () => ({
  listNotes: vi.fn(),
  getDailyNote: vi.fn(),
}));

import { listNotes } from '../../api/client';

const mockNotes = [
  {
    id: 'n1',
    title: 'Today Note',
    body: 'Content',
    file_path: 'n1.md',
    tags: ['go'],
    created_at: new Date().toISOString(),
    updated_at: new Date().toISOString(),
  },
  {
    id: 'n2',
    title: 'Yesterday Note',
    body: 'More content',
    file_path: 'n2.md',
    tags: ['api', 'rest'],
    created_at: new Date(Date.now() - 86400000).toISOString(),
    updated_at: new Date(Date.now() - 86400000).toISOString(),
  },
  {
    id: 'n3',
    title: 'Old Note',
    body: 'Old stuff',
    file_path: 'n3.md',
    tags: [],
    created_at: '2025-01-15T10:00:00Z',
    updated_at: '2025-01-15T10:00:00Z',
  },
];

describe('TimelinePage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  it('renders loading state initially', () => {
    (listNotes as ReturnType<typeof vi.fn>).mockImplementation(() => new Promise(() => {}));
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    expect(screen.getByRole('status', { name: 'Loading notes' })).toBeInTheDocument();
  });

  it('renders empty state when no notes', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: [],
      total: 0,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('No notes yet')).toBeInTheDocument();
    });
  });

  it('renders notes grouped by date', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: mockNotes,
      total: 3,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Today Note')).toBeInTheDocument();
      expect(screen.getByText('Yesterday Note')).toBeInTheDocument();
      expect(screen.getByText('Old Note')).toBeInTheDocument();
    });
  });

  it('renders title', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: [],
      total: 0,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Timeline')).toBeInTheDocument();
    });
  });

  it('renders created/modified toggle', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: mockNotes,
      total: 3,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Created')).toBeInTheDocument();
      expect(screen.getByText('Modified')).toBeInTheDocument();
    });
  });

  it('toggles sort mode between created and modified', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: mockNotes,
      total: 3,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Created')).toBeInTheDocument();
    });

    fireEvent.click(screen.getByText('Created'));

    // Should re-fetch with created sort after clicking the toggle.
    await waitFor(() => {
      expect(listNotes).toHaveBeenCalledWith(expect.objectContaining({ sort: 'created' }));
    });
  });

  it('renders tags on notes', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: mockNotes,
      total: 3,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('#go')).toBeInTheDocument();
      expect(screen.getByText('#api')).toBeInTheDocument();
    });
  });

  it('renders date picker', async () => {
    (listNotes as ReturnType<typeof vi.fn>).mockResolvedValue({
      notes: mockNotes,
      total: 3,
    });
    render(
      <MemoryRouter>
        <TimelinePage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByLabelText('Jump to date')).toBeInTheDocument();
    });
  });
});
