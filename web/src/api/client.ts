import type {
  AuthResponse,
  RegisterReq,
  LoginReq,
  TokenPair,
  Note,
  CreateNoteReq,
  UpdateNoteReq,
  NoteFilter,
  Project,
  CreateProjectReq,
  UpdateProjectReq,
  TagCount,
  FTSResult,
  SemanticResult,
  RelatedNote,
  ChatResult,
  ChatMessage,
  SynthesizeReq,
  SynthesisResult,
  CaptureURLReq,
  TemplateMeta,
  Template,
  TemplateApplyReq,
  TemplateApplyResult,
  AIAssistReq,
  AIAssistResult,
  GraphData,
  GraphFilter,
  GraphNode,
  TwoHopBacklink,
  NoteVersion,
  Conversation,
  ChatHistoryMessage,
  ResolvedLink,
  ReviewItem,
  TagSuggestion,
  ProjectSuggestion,
  BulkActionResult,
} from './types';

const BASE_URL = '/api';

let accessToken: string | null = null;
let refreshToken: string | null = null;
let onAuthFailure: (() => void) | null = null;

export function setTokens(tokens: TokenPair | null) {
  if (tokens) {
    accessToken = tokens.access_token;
    refreshToken = tokens.refresh_token;
    // Refresh token in localStorage is an accepted XSS tradeoff: the Seam
    // frontend is an SPA without a backend-for-frontend proxy, so HttpOnly
    // cookies are not feasible without significant architecture changes.
    localStorage.setItem('seam_refresh_token', tokens.refresh_token);
  } else {
    accessToken = null;
    refreshToken = null;
    localStorage.removeItem('seam_refresh_token');
  }
}

export function getAccessToken(): string | null {
  return accessToken;
}

export function getRefreshToken(): string | null {
  return refreshToken ?? localStorage.getItem('seam_refresh_token');
}

export function setOnAuthFailure(fn: () => void) {
  onAuthFailure = fn;
}

class ApiError extends Error {
  status: number;
  constructor(status: number, message: string) {
    super(message);
    this.name = 'ApiError';
    this.status = status;
  }
}

export { ApiError };

// Default request timeout in milliseconds. Individual callers can override
// via the `signal` option for long-running operations (e.g. AI endpoints).
const DEFAULT_TIMEOUT_MS = 30_000;

// Low-level request that returns the raw Response for callers that need
// headers (e.g. X-Total-Count). Auth/retry logic is shared with request().
async function requestRaw(
  path: string,
  options: RequestInit = {},
  retry = true,
): Promise<Response> {
  const headers: Record<string, string> = {
    ...(options.headers as Record<string, string>),
  };

  // Only set Content-Type to JSON when there is no body (GET) or the body
  // is a string (JSON-serialized). FormData bodies must NOT have
  // Content-Type set (the browser sets the multipart boundary).
  if (!(options.body instanceof FormData)) {
    headers['Content-Type'] = headers['Content-Type'] ?? 'application/json';
  }

  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  }

  // If the caller already supplied an AbortSignal (e.g. for streaming or
  // user cancellation), respect it. Otherwise apply a default timeout so
  // the client does not hang indefinitely when the server is unresponsive.
  let signal = options.signal;
  let timeoutId: ReturnType<typeof setTimeout> | undefined;
  if (!signal) {
    const controller = new AbortController();
    signal = controller.signal;
    timeoutId = setTimeout(() => controller.abort(), DEFAULT_TIMEOUT_MS);
  }

  let res: Response;
  try {
    res = await fetch(`${BASE_URL}${path}`, {
      ...options,
      headers,
      signal,
    });
  } finally {
    if (timeoutId !== undefined) {
      clearTimeout(timeoutId);
    }
  }

  if (res.status === 401 && retry && getRefreshToken()) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      return requestRaw(path, options, false);
    }
    onAuthFailure?.();
    throw new ApiError(401, 'Authentication failed');
  }

  if (!res.ok) {
    let message = res.statusText;
    try {
      const body = await res.json();
      message = body.error || body.message || message;
    } catch {
      // Use status text
    }
    throw new ApiError(res.status, message);
  }

  return res;
}

async function request<T>(
  path: string,
  options: RequestInit = {},
  retry = true,
): Promise<T> {
  const res = await requestRaw(path, options, retry);

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json();
}

// Deduplicate concurrent refresh requests: if multiple 401s trigger parallel
// refreshes, reuse the first in-flight promise instead of firing N requests.
let refreshPromise: Promise<boolean> | null = null;

export async function tryRefresh(): Promise<boolean> {
  if (refreshPromise) return refreshPromise;

  refreshPromise = doRefresh();
  try {
    return await refreshPromise;
  } finally {
    refreshPromise = null;
  }
}

async function doRefresh(): Promise<boolean> {
  try {
    const stored = getRefreshToken();
    if (!stored) return false;

    const res = await fetch(`${BASE_URL}/auth/refresh`, {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ refresh_token: stored }),
    });

    if (!res.ok) return false;

    const data: TokenPair = await res.json();
    accessToken = data.access_token;
    // Keep existing refresh token (server does not rotate it)
    return true;
  } catch {
    return false;
  }
}

// Auth endpoints
export async function register(req: RegisterReq): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/auth/register', {
    method: 'POST',
    body: JSON.stringify(req),
  });
  setTokens(data.tokens);
  return data;
}

export async function login(req: LoginReq): Promise<AuthResponse> {
  const data = await request<AuthResponse>('/auth/login', {
    method: 'POST',
    body: JSON.stringify(req),
  });
  setTokens(data.tokens);
  return data;
}

export async function logout(): Promise<void> {
  const stored = getRefreshToken();
  if (stored) {
    try {
      await request<void>('/auth/logout', {
        method: 'POST',
        body: JSON.stringify({ refresh_token: stored }),
      });
    } catch {
      // Ignore errors on logout
    }
  }
  setTokens(null);
}

// Note endpoints
export async function createNote(req: CreateNoteReq): Promise<Note> {
  return request<Note>('/notes/', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function getNote(id: string): Promise<Note> {
  return request<Note>(`/notes/${id}`);
}

export async function updateNote(
  id: string,
  req: UpdateNoteReq,
): Promise<Note> {
  return request<Note>(`/notes/${id}`, {
    method: 'PUT',
    body: JSON.stringify(req),
  });
}

export async function deleteNote(id: string): Promise<void> {
  return request<void>(`/notes/${id}`, { method: 'DELETE' });
}

export async function listNotes(
  filter: NoteFilter = {},
): Promise<{ notes: Note[]; total: number }> {
  const params = new URLSearchParams();
  if (filter.project) params.set('project', filter.project);
  if (filter.tag) params.set('tag', filter.tag);
  if (filter.since) params.set('since', filter.since);
  if (filter.until) params.set('until', filter.until);
  if (filter.sort) params.set('sort', filter.sort);
  if (filter.sort_dir) params.set('sort_dir', filter.sort_dir);
  if (filter.limit) params.set('limit', String(filter.limit));
  if (filter.offset != null) params.set('offset', String(filter.offset));

  const qs = params.toString();
  const path = `/notes/${qs ? `?${qs}` : ''}`;

  const res = await requestRaw(path);
  const notes: Note[] = await res.json();
  const total = parseInt(res.headers.get('X-Total-Count') ?? '0', 10);
  return { notes, total };
}

export async function getBacklinks(noteId: string): Promise<Note[]> {
  return request<Note[]>(`/notes/${noteId}/backlinks`);
}

export async function getDailyNote(date?: string): Promise<Note> {
  const param = date || 'today';
  return request<Note>(`/notes/daily?date=${encodeURIComponent(param)}`);
}

export async function appendToNote(noteId: string, text: string): Promise<Note> {
  return request<Note>(`/notes/${noteId}/append`, {
    method: 'POST',
    body: JSON.stringify({ text }),
  });
}

export async function bulkUpdateNotes(
  noteIds: string[],
  action: string,
  params: Record<string, string> = {},
): Promise<BulkActionResult> {
  return request<BulkActionResult>('/notes/bulk', {
    method: 'PATCH',
    body: JSON.stringify({ note_ids: noteIds, action, params }),
  });
}

// Project endpoints
export async function createProject(req: CreateProjectReq): Promise<Project> {
  return request<Project>('/projects/', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function getProject(id: string): Promise<Project> {
  return request<Project>(`/projects/${id}`);
}

export async function listProjects(): Promise<Project[]> {
  return request<Project[]>('/projects/');
}

export async function updateProject(
  id: string,
  req: UpdateProjectReq,
): Promise<Project> {
  return request<Project>(`/projects/${id}`, {
    method: 'PUT',
    body: JSON.stringify(req),
  });
}

export async function deleteProject(
  id: string,
  cascade: 'inbox' | 'delete',
): Promise<void> {
  return request<void>(`/projects/${id}?cascade=${cascade}`, {
    method: 'DELETE',
  });
}

// Tag endpoints
export async function listTags(): Promise<TagCount[]> {
  return request<TagCount[]>('/tags/');
}

// AI endpoints
export async function searchSemantic(
  query: string,
  limit = 10,
  signal?: AbortSignal,
): Promise<SemanticResult[]> {
  const params = new URLSearchParams({
    q: query,
    limit: String(limit),
  });
  return request<SemanticResult[]>(`/search/semantic?${params}`, { signal });
}

export async function getRelatedNotes(
  noteId: string,
  limit = 5,
): Promise<RelatedNote[]> {
  return request<RelatedNote[]>(`/ai/notes/${noteId}/related?limit=${limit}`);
}

export async function askSeam(
  query: string,
  history: ChatMessage[] = [],
): Promise<ChatResult> {
  return request<ChatResult>('/ai/ask', {
    method: 'POST',
    body: JSON.stringify({ query, history }),
  });
}

export async function synthesize(
  req: SynthesizeReq,
): Promise<SynthesisResult> {
  return request<SynthesisResult>('/ai/synthesize', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

// Capture endpoints
export async function captureURL(url: string): Promise<Note> {
  const req: CaptureURLReq = { type: 'url', url };
  return request<Note>('/capture/', {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

export async function captureVoice(audioBlob: Blob, filename = 'audio.wav'): Promise<Note> {
  const formData = new FormData();
  formData.append('audio', audioBlob, filename);

  const res = await requestRaw('/capture/', {
    method: 'POST',
    body: formData,
  });
  return res.json();
}

// Template endpoints
export async function listTemplates(): Promise<TemplateMeta[]> {
  return request<TemplateMeta[]>('/templates/');
}

export async function getTemplate(name: string): Promise<Template> {
  return request<Template>(`/templates/${encodeURIComponent(name)}`);
}

export async function applyTemplate(
  name: string,
  vars: Record<string, string> = {},
): Promise<TemplateApplyResult> {
  const req: TemplateApplyReq = { vars };
  return request<TemplateApplyResult>(`/templates/${encodeURIComponent(name)}/apply`, {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

// AI Writing Assist endpoints
export async function aiAssist(
  noteId: string,
  action: AIAssistReq['action'],
  selection?: string,
): Promise<AIAssistResult> {
  const req: AIAssistReq = { action, selection };
  return request<AIAssistResult>(`/ai/notes/${noteId}/assist`, {
    method: 'POST',
    body: JSON.stringify(req),
  });
}

// Graph endpoints
export async function getGraph(
  filter: GraphFilter = {},
): Promise<GraphData> {
  const params = new URLSearchParams();
  if (filter.project) params.set('project', filter.project);
  if (filter.tag) params.set('tag', filter.tag);
  if (filter.since) params.set('since', filter.since);
  if (filter.until) params.set('until', filter.until);
  if (filter.limit) params.set('limit', String(filter.limit));
  const qs = params.toString();
  return request<GraphData>(`/graph${qs ? `?${qs}` : ''}`);
}

export async function getTwoHopBacklinks(
  noteId: string,
): Promise<TwoHopBacklink[]> {
  return request<TwoHopBacklink[]>(`/graph/two-hop-backlinks/${noteId}`);
}

export async function getOrphanNotes(): Promise<GraphNode[]> {
  return request<GraphNode[]>('/graph/orphans');
}

// Account management endpoints
export async function getMe(): Promise<{ id: string; username: string; email: string }> {
  return request<{ id: string; username: string; email: string }>('/auth/me');
}

export async function changePassword(currentPassword: string, newPassword: string): Promise<void> {
  await request<void>('/auth/password', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ current_password: currentPassword, new_password: newPassword }),
  });
}

export async function updateEmail(email: string): Promise<void> {
  await request<void>('/auth/email', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email }),
  });
}

// Settings endpoints
export async function getSettings(): Promise<Record<string, string>> {
  return request<Record<string, string>>('/settings/');
}

export async function updateSettings(settings: Record<string, string>): Promise<void> {
  await request<void>('/settings/', {
    method: 'PUT',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify(settings),
  });
}

// Search endpoints
export async function searchFTS(
  query: string,
  limit = 20,
  offset = 0,
  signal?: AbortSignal,
): Promise<{ results: FTSResult[]; total: number }> {
  const params = new URLSearchParams({
    q: query,
    limit: String(limit),
    offset: String(offset),
  });

  const res = await requestRaw(`/search/?${params}`, { signal });
  const results: FTSResult[] = await res.json();
  const total = parseInt(res.headers.get('X-Total-Count') ?? '0', 10);
  return { results, total };
}

// Chat history endpoints

export async function createConversation(): Promise<Conversation> {
  return request<Conversation>('/chat/conversations', {
    method: 'POST',
  });
}

export async function listConversations(
  limit = 20,
  offset = 0,
): Promise<{ conversations: Conversation[]; total: number }> {
  const params = new URLSearchParams({
    limit: String(limit),
    offset: String(offset),
  });
  const res = await requestRaw(`/chat/conversations?${params}`);
  const conversations: Conversation[] = await res.json();
  const total = parseInt(res.headers.get('X-Total-Count') ?? '0', 10);
  return { conversations, total };
}

export async function getConversation(
  id: string,
): Promise<{ conversation: Conversation; messages: ChatHistoryMessage[] }> {
  return request<{ conversation: Conversation; messages: ChatHistoryMessage[] }>(
    `/chat/conversations/${id}`,
  );
}

export async function deleteConversation(id: string): Promise<void> {
  await request<void>(`/chat/conversations/${id}`, {
    method: 'DELETE',
  });
}

export async function addChatMessage(
  conversationId: string,
  message: {
    role: string;
    content: string;
    citations?: Array<{ id: string; title: string }>;
  },
): Promise<void> {
  await request<void>(`/chat/conversations/${conversationId}/messages`, {
    method: 'POST',
    body: JSON.stringify(message),
  });
}

// Version history endpoints

export async function listVersions(
  noteId: string,
  limit = 20,
  offset = 0,
): Promise<{ versions: NoteVersion[]; total: number }> {
  const res = await requestRaw(`/notes/${noteId}/versions?limit=${limit}&offset=${offset}`);
  const total = parseInt(res.headers.get('X-Total-Count') || '0', 10);
  const versions = await res.json();
  return { versions: versions || [], total };
}

export async function getVersion(
  noteId: string,
  version: number,
): Promise<NoteVersion> {
  return request<NoteVersion>(`/notes/${noteId}/versions/${version}`);
}

export async function restoreVersion(
  noteId: string,
  version: number,
): Promise<Note> {
  return request<Note>(`/notes/${noteId}/versions/${version}/restore`, {
    method: 'POST',
  });
}

// Wikilink resolution
export async function resolveWikilink(title: string): Promise<ResolvedLink> {
  return request<ResolvedLink>(`/notes/resolve?title=${encodeURIComponent(title)}`);
}

// Review queue endpoints (Knowledge Gardening)

export async function getReviewQueue(
  limit = 20,
): Promise<ReviewItem[]> {
  return request<ReviewItem[]>(`/review/queue?limit=${limit}`);
}

export async function suggestTags(
  noteId: string,
): Promise<{ tags: TagSuggestion[] }> {
  return request<{ tags: TagSuggestion[] }>('/ai/suggest-tags', {
    method: 'POST',
    body: JSON.stringify({ note_id: noteId }),
  });
}

export async function suggestProject(
  noteId: string,
): Promise<{ projects: ProjectSuggestion[] }> {
  return request<{ projects: ProjectSuggestion[] }>('/ai/suggest-project', {
    method: 'POST',
    body: JSON.stringify({ note_id: noteId }),
  });
}
