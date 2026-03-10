import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, fireEvent } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { GraphPage } from './GraphPage';

// Mock the API client.
vi.mock('../../api/client', () => ({
  getGraph: vi.fn(),
  getOrphanNotes: vi.fn().mockResolvedValue([]),
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
  const mockElement = {
    length: 0,
    position: vi.fn(),
    style: vi.fn(),
  };
  const mockCollection = {
    forEach: vi.fn(),
    show: vi.fn(),
    hide: vi.fn(),
    addClass: vi.fn().mockReturnThis(),
    removeClass: vi.fn().mockReturnThis(),
    not: vi.fn().mockReturnThis(),
    filter: vi.fn().mockReturnThis(),
  };
  const mockCy = {
    on: vi.fn(),
    one: vi.fn(),
    destroy: vi.fn(),
    fit: vi.fn(),
    zoom: vi.fn(() => 1),
    center: vi.fn(),
    extent: vi.fn(() => ({ x1: 0, y1: 0, x2: 100, y2: 100 })),
    pan: vi.fn(() => ({ x: 0, y: 0 })),
    width: vi.fn(() => 800),
    height: vi.fn(() => 600),
    getElementById: vi.fn(() => mockElement),
    add: vi.fn(),
    startBatch: vi.fn(),
    endBatch: vi.fn(),
    animate: vi.fn(),
    nodes: vi.fn(() => mockCollection),
    edges: vi.fn(() => mockCollection),
    elements: vi.fn(() => ({
      jsons: vi.fn(() => []),
      unselect: vi.fn(),
      removeClass: vi.fn().mockReturnThis(),
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

  it('renders filter panel with projects and tags after toggling', async () => {
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

    // Filter panel is collapsed by default. Click the filter toggle to open it.
    await waitFor(() => {
      expect(screen.getByTitle('Toggle filters')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTitle('Toggle filters'));

    await waitFor(() => {
      expect(screen.getByText('Filters')).toBeInTheDocument();
      expect(screen.getByText('Project One')).toBeInTheDocument();
      expect(screen.getByText('#go')).toBeInTheDocument();
      expect(screen.getByText('#api')).toBeInTheDocument();
    });
  });

  it('renders reset button after opening filters', async () => {
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
      expect(screen.getByTitle('Toggle filters')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTitle('Toggle filters'));

    await waitFor(() => {
      expect(screen.getByText('Reset filters')).toBeInTheDocument();
    });
  });

  it('renders date range inputs after opening filters', async () => {
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
      expect(screen.getByTitle('Toggle filters')).toBeInTheDocument();
    });
    fireEvent.click(screen.getByTitle('Toggle filters'));

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

  it('renders stats bar with note and link counts', async () => {
    (getGraph as ReturnType<typeof vi.fn>).mockResolvedValue({
      nodes: [
        { id: 'n1', title: 'Note A', tags: [], created_at: '2026-01-01T00:00:00Z', link_count: 1 },
        { id: 'n2', title: 'Note B', tags: [], created_at: '2026-01-01T00:00:00Z', link_count: 1 },
      ],
      edges: [{ source: 'n1', target: 'n2' }],
    });
    render(
      <MemoryRouter>
        <GraphPage />
      </MemoryRouter>,
    );
    await waitFor(() => {
      expect(screen.getByText('2 notes')).toBeInTheDocument();
      expect(screen.getByText('1 link')).toBeInTheDocument();
    });
  });

  it('renders search button', async () => {
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
      expect(screen.getByTitle('Search nodes (/)')).toBeInTheDocument();
    });
  });
});
