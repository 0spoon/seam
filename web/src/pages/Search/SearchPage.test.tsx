import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, fireEvent, waitFor, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { SearchPage } from './SearchPage';

const mockNavigate = vi.fn();

vi.mock('react-router-dom', async () => {
  const actual = await vi.importActual('react-router-dom');
  return { ...actual, useNavigate: () => mockNavigate };
});

vi.mock('../../api/client', () => ({
  searchFTS: vi.fn(),
  searchSemantic: vi.fn(),
}));

import { searchFTS, searchSemantic } from '../../api/client';

function renderSearchPage(initialRoute = '/search') {
  return render(
    <MemoryRouter initialEntries={[initialRoute]}>
      <SearchPage />
    </MemoryRouter>,
  );
}

describe('SearchPage', () => {
  beforeEach(() => {
    vi.clearAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('renders search input', () => {
    renderSearchPage();
    expect(screen.getByLabelText('Search notes')).toBeInTheDocument();
  });

  it('defaults to fulltext mode', () => {
    renderSearchPage();
    const fulltextTab = screen.getByText('Full-text');
    expect(fulltextTab.className).toContain('activeTab');
  });

  it('shows placeholder for fulltext mode', () => {
    renderSearchPage();
    expect(screen.getByPlaceholderText('Search notes...')).toBeInTheDocument();
  });

  it('switches to semantic mode and updates placeholder', () => {
    renderSearchPage();
    const semanticTab = screen.getByText('Semantic');
    fireEvent.click(semanticTab);
    expect(
      screen.getByPlaceholderText('Ask a question about your notes...'),
    ).toBeInTheDocument();
  });

  it('calls searchFTS in fulltext mode after debounce', async () => {
    vi.mocked(searchFTS).mockResolvedValue({
      results: [
        { note_id: 'n1', title: 'API Design', snippet: 'REST <b>API</b>', rank: 1.0 },
      ],
      total: 1,
    });

    renderSearchPage();

    const input = screen.getByLabelText('Search notes');
    await act(async () => {
      fireEvent.change(input, { target: { value: 'API' } });
    });

    await waitFor(
      () => {
        expect(searchFTS).toHaveBeenCalledWith('API', 20);
      },
      { timeout: 2000 },
    );

    await waitFor(() => {
      expect(screen.getByText('API Design')).toBeInTheDocument();
    });
  });

  it('calls searchSemantic in semantic mode after debounce', async () => {
    vi.mocked(searchSemantic).mockResolvedValue([
      { note_id: 'n1', title: 'Caching Guide', snippet: 'about caching', score: 0.85 },
    ]);

    renderSearchPage();

    fireEvent.click(screen.getByText('Semantic'));

    const input = screen.getByLabelText('Search notes');
    await act(async () => {
      fireEvent.change(input, { target: { value: 'caching' } });
    });

    await waitFor(
      () => {
        expect(searchSemantic).toHaveBeenCalledWith('caching', 20);
      },
      { timeout: 2000 },
    );

    await waitFor(() => {
      expect(screen.getByText('Caching Guide')).toBeInTheDocument();
      expect(screen.getByText('85%')).toBeInTheDocument();
    });
  });

  it('navigates to note on result click', async () => {
    vi.mocked(searchFTS).mockResolvedValue({
      results: [
        { note_id: 'note123', title: 'My Note', snippet: 'content', rank: 1.0 },
      ],
      total: 1,
    });

    renderSearchPage();

    await act(async () => {
      fireEvent.change(screen.getByLabelText('Search notes'), {
        target: { value: 'test' },
      });
    });

    await waitFor(
      () => {
        expect(screen.getByText('My Note')).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    fireEvent.click(screen.getByText('My Note'));
    expect(mockNavigate).toHaveBeenCalledWith('/notes/note123');
  });

  it('shows empty state when no results match', async () => {
    vi.mocked(searchFTS).mockResolvedValue({ results: [], total: 0 });

    renderSearchPage();

    await act(async () => {
      fireEvent.change(screen.getByLabelText('Search notes'), {
        target: { value: 'zzz' },
      });
    });

    await waitFor(
      () => {
        expect(screen.getByText('No matches')).toBeInTheDocument();
        expect(screen.getByText('Try different keywords')).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
  });

  it('shows semantic-specific empty state subtext', async () => {
    vi.mocked(searchSemantic).mockResolvedValue([]);

    renderSearchPage();
    fireEvent.click(screen.getByText('Semantic'));

    await act(async () => {
      fireEvent.change(screen.getByLabelText('Search notes'), {
        target: { value: 'zzz' },
      });
    });

    await waitFor(
      () => {
        expect(screen.getByText('Try rephrasing your question')).toBeInTheDocument();
      },
      { timeout: 2000 },
    );
  });

  it('clears results when query is emptied', async () => {
    vi.mocked(searchFTS).mockResolvedValue({
      results: [
        { note_id: 'n1', title: 'Result', snippet: 'text', rank: 1.0 },
      ],
      total: 1,
    });

    renderSearchPage();
    const input = screen.getByLabelText('Search notes');

    await act(async () => {
      fireEvent.change(input, { target: { value: 'test' } });
    });

    await waitFor(
      () => {
        expect(screen.getByText('Result')).toBeInTheDocument();
      },
      { timeout: 2000 },
    );

    // Clear the query.
    await act(async () => {
      fireEvent.change(input, { target: { value: '' } });
    });

    await waitFor(() => {
      expect(screen.queryByText('Result')).not.toBeInTheDocument();
    });
  });
});
