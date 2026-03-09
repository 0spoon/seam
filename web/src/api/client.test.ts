import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  setTokens,
  getAccessToken,
  getRefreshToken,
  setOnAuthFailure,
  register,
  login,
  logout,
  createNote,
  getNote,
  updateNote,
  deleteNote,
  listProjects,
  searchFTS,
  searchSemantic,
  askSeam,
  synthesize,
  ApiError,
} from './client';

describe('API Client', () => {
  beforeEach(() => {
    setTokens(null);
    localStorage.clear();
    vi.restoreAllMocks();
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  describe('Token management', () => {
    it('stores and retrieves tokens', () => {
      setTokens({ access_token: 'access123', refresh_token: 'refresh456' });
      expect(getAccessToken()).toBe('access123');
      expect(getRefreshToken()).toBe('refresh456');
    });

    it('persists refresh token in localStorage', () => {
      setTokens({ access_token: 'a', refresh_token: 'r' });
      expect(localStorage.getItem('seam_refresh_token')).toBe('r');
    });

    it('clears tokens on null', () => {
      setTokens({ access_token: 'a', refresh_token: 'r' });
      setTokens(null);
      expect(getAccessToken()).toBeNull();
      expect(localStorage.getItem('seam_refresh_token')).toBeNull();
    });

    it('reads refresh token from localStorage as fallback', () => {
      localStorage.setItem('seam_refresh_token', 'stored_refresh');
      expect(getRefreshToken()).toBe('stored_refresh');
    });
  });

  describe('register', () => {
    it('calls the register endpoint and stores tokens', async () => {
      const mockResponse = {
        user: { id: '1', username: 'test', email: 'test@example.com' },
        tokens: { access_token: 'at', refresh_token: 'rt' },
      };

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResponse), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await register({
        username: 'test',
        email: 'test@example.com',
        password: 'pass',
      });

      expect(result.user.username).toBe('test');
      expect(getAccessToken()).toBe('at');
      expect(getRefreshToken()).toBe('rt');
    });

    it('throws ApiError on failure', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'username taken' }), {
          status: 409,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await expect(
        register({ username: 'test', email: 'test@example.com', password: 'pass' }),
      ).rejects.toThrow(ApiError);
    });
  });

  describe('login', () => {
    it('calls the login endpoint and stores tokens', async () => {
      const mockResponse = {
        user: { id: '1', username: 'test', email: 'test@example.com' },
        tokens: { access_token: 'at2', refresh_token: 'rt2' },
      };

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResponse), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await login({ username: 'test', password: 'pass' });
      expect(result.user.username).toBe('test');
      expect(getAccessToken()).toBe('at2');
    });
  });

  describe('logout', () => {
    it('clears tokens after logout', async () => {
      setTokens({ access_token: 'a', refresh_token: 'r' });

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(null, { status: 204 }),
      );

      await logout();
      expect(getAccessToken()).toBeNull();
      expect(localStorage.getItem('seam_refresh_token')).toBeNull();
    });
  });

  describe('authenticated requests', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('sends Authorization header', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await listProjects();

      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/projects/',
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer valid_token',
          }),
        }),
      );
    });

    it('creates a note', async () => {
      const mockNote = {
        id: 'note1',
        title: 'Test',
        body: 'content',
        file_path: 'test.md',
        tags: [],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await createNote({ title: 'Test', body: 'content' });
      expect(result.id).toBe('note1');
    });

    it('gets a note', async () => {
      const mockNote = {
        id: 'note1',
        title: 'Test',
        body: 'content',
        file_path: 'test.md',
        tags: ['tag1'],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await getNote('note1');
      expect(result.title).toBe('Test');
      expect(result.tags).toEqual(['tag1']);
    });

    it('updates a note', async () => {
      const mockNote = {
        id: 'note1',
        title: 'Updated',
        body: 'new content',
        file_path: 'test.md',
        tags: [],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:01Z',
      };

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await updateNote('note1', { title: 'Updated', body: 'new content' });
      expect(result.title).toBe('Updated');
    });

    it('deletes a note', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(null, { status: 204 }),
      );

      await expect(deleteNote('note1')).resolves.toBeUndefined();
    });

    it('searches FTS', async () => {
      const mockResults = [
        { note_id: 'n1', title: 'Result', snippet: 'matched <b>text</b>', rank: 1.0 },
      ];

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResults), {
          status: 200,
          headers: {
            'Content-Type': 'application/json',
            'X-Total-Count': '1',
          },
        }),
      );

      const { results, total } = await searchFTS('text');
      expect(results).toHaveLength(1);
      expect(results[0].title).toBe('Result');
      expect(total).toBe(1);
    });
  });

  describe('searchSemantic', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('returns semantic search results', async () => {
      const mockResults = [
        { note_id: 'n1', title: 'Caching', score: 0.85, snippet: 'about caching' },
        { note_id: 'n2', title: 'APIs', score: 0.6, snippet: 'rest apis' },
      ];

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResults), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const results = await searchSemantic('caching', 10);
      expect(results).toHaveLength(2);
      expect(results[0].note_id).toBe('n1');
      expect(results[0].score).toBe(0.85);

      expect(fetchSpy).toHaveBeenCalledWith(
        expect.stringContaining('/api/search/semantic?'),
        expect.objectContaining({
          headers: expect.objectContaining({
            Authorization: 'Bearer valid_token',
          }),
        }),
      );
    });

    it('passes query and limit as URL params', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify([]), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await searchSemantic('my query', 5);

      const calledUrl = fetchSpy.mock.calls[0][0] as string;
      expect(calledUrl).toContain('q=my+query');
      expect(calledUrl).toContain('limit=5');
    });
  });

  describe('askSeam', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('sends query and returns response with citations', async () => {
      const mockResult = {
        response: 'Caching improves performance.',
        citations: ['note1', 'note2'],
      };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await askSeam('What is caching?');
      expect(result.response).toBe('Caching improves performance.');
      expect(result.citations).toEqual(['note1', 'note2']);

      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/ai/ask',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({ query: 'What is caching?', history: [] }),
        }),
      );
    });

    it('passes conversation history', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ response: 'ok', citations: [] }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const history = [
        { role: 'user' as const, content: 'first question' },
        { role: 'assistant' as const, content: 'first answer' },
      ];
      await askSeam('follow up', history);

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.history).toHaveLength(2);
      expect(body.query).toBe('follow up');
    });

    it('throws ApiError on server error', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'model unavailable' }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await expect(askSeam('test')).rejects.toThrow(ApiError);
    });
  });

  describe('synthesize', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('sends synthesis request and returns response', async () => {
      const mockResult = { response: 'Key themes: caching, APIs, testing' };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await synthesize({
        scope: 'tag',
        tag: 'architecture',
        prompt: 'Summarize themes',
      });
      expect(result.response).toBe('Key themes: caching, APIs, testing');

      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/ai/synthesize',
        expect.objectContaining({
          method: 'POST',
          body: JSON.stringify({
            scope: 'tag',
            tag: 'architecture',
            prompt: 'Summarize themes',
          }),
        }),
      );
    });

    it('sends project-scoped synthesis', async () => {
      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ response: 'done' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await synthesize({
        scope: 'project',
        project_id: 'proj1',
        prompt: 'Summarize',
      });

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.scope).toBe('project');
      expect(body.project_id).toBe('proj1');
    });
  });

  describe('401 handling', () => {
    it('calls onAuthFailure when refresh fails', async () => {
      setTokens({ access_token: 'expired', refresh_token: 'bad_rt' });
      const authFailure = vi.fn();
      setOnAuthFailure(authFailure);

      // First call returns 401
      vi.spyOn(globalThis, 'fetch')
        .mockResolvedValueOnce(new Response(null, { status: 401 }))
        // Refresh attempt also fails
        .mockResolvedValueOnce(new Response(null, { status: 401 }));

      await expect(listProjects()).rejects.toThrow(ApiError);
      expect(authFailure).toHaveBeenCalled();
    });
  });
});
