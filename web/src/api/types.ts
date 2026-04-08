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
  // undefined (or field omitted) = no change to project assignment.
  // empty string "" = move note to inbox (remove from project).
  project_id?: string;
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
  role: 'user' | 'assistant';
  content: string;
}

export interface ChatCitation {
  id: string;
  title: string;
}

export interface ChatResult {
  response: string;
  citations: ChatCitation[];
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

// Phase 4: Graph types
export interface GraphNode {
  id: string;
  title: string;
  project_id?: string;
  project?: string; // human-readable project name
  tags: string[];
  created_at: string;
  link_count: number;
}

export interface GraphEdge {
  source: string;
  target: string;
}

export interface GraphData {
  nodes: GraphNode[];
  edges: GraphEdge[];
}

export interface GraphFilter {
  project?: string;
  tag?: string;
  since?: string;
  until?: string;
  limit?: number;
}

// Phase 2: Chat history types
export interface Conversation {
  id: string;
  title: string;
  created_at: string;
  updated_at: string;
}

export interface ChatHistoryMessage {
  id: string;
  conversation_id: string;
  role: 'user' | 'assistant' | 'tool' | 'system';
  content: string;
  citations?: ChatCitation[];
  tool_calls?: AssistantToolCall[];
  tool_call_id?: string;
  tool_name?: string;
  iteration?: number;
  created_at: string;
}

// Agentic assistant types (SSE chat stream + tool invocations).

export type AssistantStreamEventType =
  | 'text'
  | 'tool_use'
  | 'confirmation'
  | 'done'
  | 'error';

export interface AssistantStreamEvent {
  type: AssistantStreamEventType;
  content?: string;
  tool_name?: string;
  error?: string;
  iterations?: number;
}

export interface AssistantToolCall {
  id: string;
  name: string;
  arguments: string;
}

// AssistantMessage mirrors the server's chat.Message wire format. It is
// what the client sends back as `history` and what getConversation returns
// when reloading an agentic conversation.
export interface AssistantMessage {
  role: 'user' | 'assistant' | 'tool' | 'system';
  content: string;
  tool_calls?: AssistantToolCall[];
  tool_call_id?: string;
  tool_name?: string;
}

// AssistantToolResult is the response from approve/reject endpoints.
export interface AssistantToolResult {
  tool_name: string;
  arguments?: unknown;
  result?: unknown;
  error?: string;
  duration_ms: number;
}

// ToolCallView is the local UI state for one tool invocation in the chat.
export interface ToolCallView {
  id: string; // local ULID, not the server's tool_call_id
  toolName: string;
  status: 'running' | 'ok' | 'error';
  resultJson?: string; // raw JSON string the renderer parses
  errorMessage?: string;
}

// Feature 9: Note version history
export interface NoteVersion {
  id: string;
  note_id: string;
  version: number;
  title: string;
  body: string;
  content_hash: string;
  created_at: string;
}

// Feature 2: Wikilink resolution
export interface ResolvedLink {
  dangling: boolean;
  note_id?: string;
  title: string;
  snippet?: string;
  tags?: string[];
}

// Feature 8: Bulk operations
export interface BulkActionResult {
  success: number;
  failed: number;
  errors?: string[];
}

// Phase 4: Two-hop backlinks (includes the intermediate connecting note)
export interface TwoHopBacklink {
  id: string;
  title: string;
  via_id: string;    // intermediate note ID
  via_title: string; // intermediate note title
}

// Feature 3: Knowledge Gardening types
export interface ReviewItem {
  type: 'orphan' | 'untagged' | 'inbox' | 'similar_pair';
  note_id: string;
  note_title: string;
  note_snippet: string;
  suggestions: ReviewSuggestion[];
  created_at: string;
}

export interface ReviewSuggestion {
  action: string;
  target: string;
  reason: string;
}

export interface TagSuggestion {
  name: string;
  confidence: number;
}

export interface ProjectSuggestion {
  id: string;
  name: string;
  confidence: number;
}
