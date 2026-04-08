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
  captureURL,
  listTemplates,
  applyTemplate,
  aiAssist,
  getGraph,
  getTwoHopBacklinks,
  getOrphanNotes,
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

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(null, { status: 204 }));

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
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(new Response(null, { status: 204 }));

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

  describe('captureURL', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('sends URL capture request and returns note', async () => {
      const mockNote = {
        id: 'cap1',
        title: 'Example Page',
        body: 'Page content extracted from URL.',
        file_path: 'inbox/example-page.md',
        source_url: 'https://example.com/article',
        tags: [],
        created_at: '2026-01-01T00:00:00Z',
        updated_at: '2026-01-01T00:00:00Z',
      };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNote), {
          status: 201,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await captureURL('https://example.com/article');
      expect(result.id).toBe('cap1');
      expect(result.title).toBe('Example Page');

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.type).toBe('url');
      expect(body.url).toBe('https://example.com/article');
    });

    it('throws ApiError on failure', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'SSRF blocked' }), {
          status: 400,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await expect(captureURL('http://localhost/secret')).rejects.toThrow(ApiError);
    });
  });

  describe('listTemplates', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('returns list of templates', async () => {
      const mockTemplates = [
        { name: 'meeting-notes', description: 'Meeting notes template' },
        { name: 'daily-log', description: 'Daily log template' },
      ];

      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockTemplates), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await listTemplates();
      expect(result).toHaveLength(2);
      expect(result[0].name).toBe('meeting-notes');
      expect(result[1].description).toBe('Daily log template');
    });
  });

  describe('applyTemplate', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('applies template with vars and returns body', async () => {
      const mockResult = { body: '# Meeting Notes\n\nDate: 2026-03-08\n\n## Agenda\n\n' };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await applyTemplate('meeting-notes', { title: 'Sprint Review' });
      expect(result.body).toContain('Meeting Notes');

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.vars.title).toBe('Sprint Review');
    });

    it('applies template with empty vars', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ body: '# Daily Log\n' }), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await applyTemplate('daily-log');
      expect(result.body).toContain('Daily Log');
    });

    it('throws ApiError when template not found', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'template not found' }), {
          status: 404,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await expect(applyTemplate('nonexistent')).rejects.toThrow(ApiError);
    });
  });

  describe('aiAssist', () => {
    beforeEach(() => {
      setTokens({ access_token: 'valid_token', refresh_token: 'rt' });
    });

    it('sends expand action with selection', async () => {
      const mockResult = { result: 'Expanded paragraph about caching strategies...' };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await aiAssist('note1', 'expand', '- caching is important');
      expect(result.result).toContain('Expanded paragraph');

      expect(fetchSpy).toHaveBeenCalledWith(
        '/api/ai/notes/note1/assist',
        expect.objectContaining({ method: 'POST' }),
      );

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.action).toBe('expand');
      expect(body.selection).toBe('- caching is important');
    });

    it('sends summarize action without selection', async () => {
      const mockResult = { result: 'This note covers three main topics...' };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await aiAssist('note2', 'summarize');
      expect(result.result).toContain('three main topics');

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.action).toBe('summarize');
      expect(body.selection).toBeUndefined();
    });

    it('sends extract-actions action', async () => {
      const mockResult = { result: '- [ ] Review PR\n- [ ] Update docs' };

      const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockResult), {
          status: 200,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      const result = await aiAssist(
        'note3',
        'extract-actions',
        'We need to review the PR and update the docs.',
      );
      expect(result.result).toContain('Review PR');

      const body = JSON.parse(fetchSpy.mock.calls[0][1]?.body as string);
      expect(body.action).toBe('extract-actions');
    });

    it('throws ApiError on failure', async () => {
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify({ error: 'model unavailable' }), {
          status: 500,
          headers: { 'Content-Type': 'application/json' },
        }),
      );

      await expect(aiAssist('note1', 'expand')).rejects.toThrow(ApiError);
    });
  });

  describe('getGraph', () => {
    it('fetches graph data with no filter', async () => {
      setTokens({ access_token: 'token', refresh_token: 'rt' });
      const mockGraph = {
        nodes: [{ id: 'n1', title: 'Test', tags: [], link_count: 0 }],
        edges: [],
      };
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockGraph), { status: 200 }),
      );
      const result = await getGraph();
      expect(result.nodes).toHaveLength(1);
      expect(result.edges).toHaveLength(0);
    });

    it('fetches graph data with filter', async () => {
      setTokens({ access_token: 'token', refresh_token: 'rt' });
      const mockGraph = { nodes: [], edges: [] };
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockGraph), { status: 200 }),
      );
      const result = await getGraph({ tag: 'go', limit: 100 });
      expect(result).toBeDefined();
      const url = (globalThis.fetch as ReturnType<typeof vi.fn>).mock.calls[0][0] as string;
      expect(url).toContain('tag=go');
      expect(url).toContain('limit=100');
    });
  });

  describe('getTwoHopBacklinks', () => {
    it('fetches two-hop backlinks for a note', async () => {
      setTokens({ access_token: 'token', refresh_token: 'rt' });
      const mockNodes = [{ id: 'n2', title: 'Two Hops Away' }];
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNodes), { status: 200 }),
      );
      const result = await getTwoHopBacklinks('n1');
      expect(result).toHaveLength(1);
      expect(result[0].id).toBe('n2');
    });
  });

  describe('getOrphanNotes', () => {
    it('fetches orphan notes', async () => {
      setTokens({ access_token: 'token', refresh_token: 'rt' });
      const mockNodes = [{ id: 'n3', title: 'Orphan' }];
      vi.spyOn(globalThis, 'fetch').mockResolvedValueOnce(
        new Response(JSON.stringify(mockNodes), { status: 200 }),
      );
      const result = await getOrphanNotes();
      expect(result).toHaveLength(1);
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
