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
} from './types';

const BASE_URL = '/api';

let accessToken: string | null = null;
let refreshToken: string | null = null;
let onAuthFailure: (() => void) | null = null;

export function setTokens(tokens: TokenPair | null) {
  if (tokens) {
    accessToken = tokens.access_token;
    refreshToken = tokens.refresh_token;
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

async function request<T>(
  path: string,
  options: RequestInit = {},
  retry = true,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...(options.headers as Record<string, string>),
  };

  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  }

  const res = await fetch(`${BASE_URL}${path}`, {
    ...options,
    headers,
  });

  if (res.status === 401 && retry && refreshToken) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      return request<T>(path, options, false);
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

  if (res.status === 204) {
    return undefined as T;
  }

  return res.json();
}

async function tryRefresh(): Promise<boolean> {
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
  if (filter.offset) params.set('offset', String(filter.offset));

  const qs = params.toString();
  const path = `/notes/${qs ? `?${qs}` : ''}`;

  const res = await fetch(`${BASE_URL}${path}`, {
    headers: {
      'Content-Type': 'application/json',
      ...(accessToken ? { Authorization: `Bearer ${accessToken}` } : {}),
    },
  });

  if (res.status === 401) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      return listNotes(filter);
    }
    onAuthFailure?.();
    throw new ApiError(401, 'Authentication failed');
  }

  if (!res.ok) {
    throw new ApiError(res.status, res.statusText);
  }

  const notes: Note[] = await res.json();
  const total = parseInt(res.headers.get('X-Total-Count') ?? '0', 10);
  return { notes, total };
}

export async function getBacklinks(noteId: string): Promise<Note[]> {
  return request<Note[]>(`/notes/${noteId}/backlinks`);
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
): Promise<SemanticResult[]> {
  const params = new URLSearchParams({
    q: query,
    limit: String(limit),
  });
  return request<SemanticResult[]>(`/search/semantic?${params}`);
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

  const headers: Record<string, string> = {};
  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  }

  const res = await fetch(`${BASE_URL}/capture/`, {
    method: 'POST',
    headers,
    body: formData,
  });

  if (res.status === 401) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      return captureVoice(audioBlob, filename);
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

  return res.json();
}

// Template endpoints
export async function listTemplates(): Promise<TemplateMeta[]> {
  return request<TemplateMeta[]>('/templates/');
}

export async function getTemplate(name: string): Promise<Template> {
  return request<Template>(`/templates/${name}`);
}

export async function applyTemplate(
  name: string,
  vars: Record<string, string> = {},
): Promise<TemplateApplyResult> {
  const req: TemplateApplyReq = { vars };
  return request<TemplateApplyResult>(`/templates/${name}/apply`, {
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

// Search endpoints
export async function searchFTS(
  query: string,
  limit = 20,
  offset = 0,
): Promise<{ results: FTSResult[]; total: number }> {
  const params = new URLSearchParams({
    q: query,
    limit: String(limit),
    offset: String(offset),
  });

  const path = `/search/?${params}`;

  // Use the request() helper with a custom response handler to read
  // X-Total-Count header. We need to use fetch directly for the header,
  // but go through the same JWT interceptor logic.
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  };
  if (accessToken) {
    headers['Authorization'] = `Bearer ${accessToken}`;
  }

  let res = await fetch(`${BASE_URL}${path}`, { headers });

  // Handle 401 with token refresh (same logic as request() helper).
  if (res.status === 401 && refreshToken) {
    const refreshed = await tryRefresh();
    if (refreshed) {
      headers['Authorization'] = `Bearer ${accessToken}`;
      res = await fetch(`${BASE_URL}${path}`, { headers });
    } else {
      onAuthFailure?.();
      throw new ApiError(401, 'Authentication failed');
    }
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

  const results: FTSResult[] = await res.json();
  const total = parseInt(res.headers.get('X-Total-Count') ?? '0', 10);
  return { results, total };
}
