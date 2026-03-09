export interface UserInfo {
  id: string;
  username: string;
  email: string;
}

export interface TokenPair {
  access_token: string;
  refresh_token: string;
}

export interface AuthResponse {
  user: UserInfo;
  tokens: TokenPair;
}

export interface RegisterReq {
  username: string;
  email: string;
  password: string;
}

export interface LoginReq {
  username: string;
  password: string;
}

export interface Note {
  id: string;
  title: string;
  project_id?: string;
  file_path: string;
  body: string;
  source_url?: string;
  transcript_source?: boolean;
  tags: string[];
  created_at: string;
  updated_at: string;
}

export interface CreateNoteReq {
  title: string;
  body: string;
  project_id?: string;
  tags?: string[];
  source_url?: string;
  template?: string;
}

export interface UpdateNoteReq {
  title?: string;
  body?: string;
  project_id?: string | null;
  tags?: string[];
}

export interface NoteFilter {
  project?: string;
  tag?: string;
  since?: string;
  until?: string;
  sort?: 'created' | 'modified';
  sort_dir?: 'asc' | 'desc';
  limit?: number;
  offset?: number;
}

export interface Project {
  id: string;
  name: string;
  slug: string;
  description: string;
  created_at: string;
  updated_at: string;
}

export interface CreateProjectReq {
  name: string;
  description?: string;
}

export interface UpdateProjectReq {
  name?: string;
  description?: string;
}

export interface TagCount {
  name: string;
  count: number;
}

export interface FTSResult {
  note_id: string;
  title: string;
  snippet: string;
  rank: number;
}

export interface SearchResponse {
  results: FTSResult[];
  total: number;
}

export interface SemanticResult {
  note_id: string;
  title: string;
  score: number;
  snippet: string;
}

export interface RelatedNote {
  note_id: string;
  title: string;
  score: number;
}

export interface ChatMessage {
  role: 'system' | 'user' | 'assistant';
  content: string;
}

export interface ChatResult {
  response: string;
  citations: string[];
}

export interface SynthesizeReq {
  scope: 'project' | 'tag';
  project_id?: string;
  tag?: string;
  prompt: string;
}

export interface SynthesisResult {
  response: string;
}

export interface LinkSuggestion {
  target_note_id: string;
  target_title: string;
  reason: string;
}

export interface WSMessage {
  type: string;
  payload: unknown;
}

// Phase 3: Capture types
export interface CaptureURLReq {
  type: 'url';
  url: string;
}

// Phase 3: Template types
export interface TemplateMeta {
  name: string;
  description: string;
}

export interface Template {
  name: string;
  description: string;
  body: string;
}

export interface TemplateApplyReq {
  vars: Record<string, string>;
}

export interface TemplateApplyResult {
  body: string;
}

// Phase 3: AI Writing Assist types
export interface AIAssistReq {
  action: 'expand' | 'summarize' | 'extract-actions';
  selection?: string;
}

export interface AIAssistResult {
  result: string;
}
