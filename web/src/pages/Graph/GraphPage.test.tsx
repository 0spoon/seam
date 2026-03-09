import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { GraphPage } from './GraphPage';

// Mock the API client.
vi.mock('../../api/client', () => ({
  getGraph: vi.fn(),
}));

// Mock the stores.
vi.mock('../../stores/projectStore', () => ({
  useProjectStore: vi.fn((selector) =>
    selector({
      projects: [
        { id: 'p1', name: 'Project One', slug: 'project-one' },
      ],
    }),
  ),
}));

vi.mock('../../stores/uiStore', () => ({
  useUIStore: vi.fn((selector) =>
    selector({
      tags: [{ name: 'go', count: 5 }, { name: 'api', count: 3 }],
    }),
  ),
}));

// Mock cytoscape to avoid canvas issues in JSDOM.
vi.mock('cytoscape', () => {
  const mockCy = {
    on: vi.fn(),
    destroy: vi.fn(),
    fit: vi.fn(),
    nodes: vi.fn(() => ({
      forEach: vi.fn(),
      show: vi.fn(),
    })),
    edges: vi.fn(() => ({
      forEach: vi.fn(),
      show: vi.fn(),
    })),
    elements: vi.fn(() => ({
      jsons: vi.fn(() => []),
      unselect: vi.fn(),
    })),
  };
  const cytoscapeFn = vi.fn(() => mockCy);
  (cytoscapeFn as unknown as { use: ReturnType<typeof vi.fn> }).use = vi.fn();
  return { default: cytoscapeFn };
});

vi.mock('cytoscape-fcose', () => ({ default: vi.fn() }));

import { getGraph } from '../../api/client';

describe('GraphPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    Object.defineProperty(window, 'matchMedia', {
      writable: true,
      value: vi.fn().mockImplementation((query: string) => ({
        matches: false,
        media: query,
        onchange: null,
        addListener: vi.fn(),
        removeListener: vi.fn(),
        addEventListener: vi.fn(),
        removeEventListener: vi.fn(),
        dispatchEvent: vi.fn(),
      })),
    });
  });

  it('renders loading state initially', () => {
    (getGraph as ReturnType<typeof vi.fn>).mockImplementation(
      () => new Promise(() => {}), // never resolves
    );
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    expect(screen.getByRole('status', { name: 'Loading graph' })).toBeInTheDocument();
  });

  it('renders empty state when no nodes', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [],
      edges: [],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('No notes yet')).toBeInTheDocument();
    });
  });

  it('renders filter panel with projects and tags', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [
        { id: 'n1', title: 'Test', project_id: 'p1', tags: ['go'], created_at: '2026-01-01T00:00:00Z', link_count: 1 },
      ],
      edges: [],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Filters')).toBeInTheDocument();
      expect(screen.getByText('Project One')).toBeInTheDocument();
      expect(screen.getByText('#go')).toBeInTheDocument();
      expect(screen.getByText('#api')).toBeInTheDocument();
    });
  });

  it('renders reset button', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [
        { id: 'n1', title: 'Test', tags: [], created_at: '2026-01-01T00:00:00Z', link_count: 0 },
      ],
      edges: [],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('Reset filters')).toBeInTheDocument();
    });
  });

  it('renders date range inputs', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [
        { id: 'n1', title: 'Test', tags: [], created_at: '2026-01-01T00:00:00Z', link_count: 0 },
      ],
      edges: [],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByLabelText('Since date')).toBeInTheDocument();
      expect(screen.getByLabelText('Until date')).toBeInTheDocument();
    });
  });

  it('calls getGraph on mount', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [],
      edges: [],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(getGraph).toHaveBeenCalledTimes(1);
    });
  });
});
