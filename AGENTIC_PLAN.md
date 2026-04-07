# Agentic Plan: Smart Personal AI Assistant

This document describes the features, components, and architectural changes needed to evolve Seam from a reactive knowledge system into a proactive, agentic personal AI assistant.

## Status (2026-04-06)

| Phase | Name | Status |
|---|---|---|
| 1 | Agentic Loop with Tool Use | **Complete** |
| 2 | User Profile + Long-Term Memory | **Complete** |
| 3 | Scheduled Triggers + Daily Briefing | Not started |
| 4 | Reminders + Due Dates | Not started |
| 5 | Event-Driven Automations | Not started |
| 6 | Document Ingestion | Not started |
| 7 | External Integrations | Not started |
| 8 | Notifications | Not started |
| 9 | Planning + Goal Tracking | Not started |
| 10 | Learning + Personalization | Not started |

## Changelog

- **2026-04-06**: Phase 1 (Agentic Loop) shipped: `internal/assistant/` package (~4,200 LOC) with tool registry, agentic loop, SSE streaming, confirmation workflow, action audit trail. All three LLM providers (Ollama, OpenAI, Anthropic) implement `ai.ToolChatCompleter`. HTTP routes mounted at `/api/assistant`.
- **2026-04-06**: Phase 2 (User Profile + Long-Term Memory) shipped: `memories` table with FTS5 search, `user_profile` key-value table, profile + memory CRUD, system prompt enrichment, memory tools (`save_memory`, `search_memories`, `get_profile`, `update_profile`).
- **2026-04-06**: Phase 2.2 (memory decay): `SearchMemories` now ranks results by a composite of FTS5 BM25 score, true 30-day half-life decay over `last_accessed`/`created_at`, and the memory's `confidence`. `loadContext` calls `TouchMemories` so frequently-recalled items stay fresh.
- **2026-04-06**: Phase 2.3 (conversation summarization): legacy `internal/ai/chat.go` 5-turn cap raised to a 20-message recent window. `BuildChatMessages`, `Ask`, `AskStream`, `/api/ai/ask`, and the WebSocket chat path all accept an optional `summary` field. New `ChatService.SummarizeHistory` produces digests via the LLM. The assistant `Service.Chat` automatically loads a per-conversation summary memory (category `summary`, deterministic ID `conv_summary_{id}`) for long histories, slices history to the recent window, embeds the summary in the system prompt, and triggers a background refresh after the call when the conversation has grown past the recent window plus a buffer.

## Current State

Seam already provides a solid foundation:

- **Knowledge base**: Markdown notes on disk, FTS5 + semantic search, wikilinks, knowledge graph
- **RAG chat**: Context-grounded Q&A over your notes (Ollama, OpenAI, Anthropic)
- **Background AI queue**: Priority-based task processing with retries and WebSocket progress
- **Multi-provider LLM**: Ollama (local), OpenAI, Anthropic with streaming support
- **Agent memory + MCP**: Session management, knowledge persistence, tool call audit logging
- **Writing assist**: Expand, summarize, extract actions
- **Webhooks**: Event-driven integration hooks for external systems

The core limitation: Seam is **purely reactive**. The user must ask every question and trigger every action. A personal AI assistant anticipates needs, takes actions, and maintains continuity across interactions.

---

## Phase 1: Agentic Loop with Tool Use

**Status**: **Complete** (2026-04-06)

**Goal**: Let the chat AI call tools and iterate, turning it from a text generator into an agent that can act on your behalf.

**Priority**: Critical -- this is the single biggest gap and the foundation for everything else.

### 1.1 Assistant Service (`internal/assistant/`)

Create a new package that wraps the existing AI chat with a tool-use loop.

- **`assistant.go`** -- Core agentic loop:
  - Receives user message + conversation history
  - Constructs system prompt with user profile, current date/time, and available tools
  - Calls LLM with tool definitions (function calling)
  - When LLM returns a tool call, executes it against internal services
  - Feeds tool results back to LLM for next iteration
  - Loops until LLM produces a final text response (max iterations capped, e.g. 10)
  - Streams partial text responses to the user via WebSocket/SSE

- **`tools.go`** -- Internal tool registry mapping tool names to service methods:
  - `search_notes` -- FTS and semantic search
  - `read_note` -- Get full note content
  - `create_note` -- Create a new note
  - `update_note` -- Modify note title, body, tags, project
  - `append_to_note` -- Append timestamped content
  - `list_notes` -- List/filter notes
  - `list_projects` -- List projects
  - `create_project` -- Create a project
  - `list_tasks` -- List tasks with filters
  - `toggle_task` -- Mark task done/undone
  - `get_daily_note` -- Get or create today's daily note
  - `get_graph` -- Query knowledge graph
  - `find_related` -- Find semantically related notes
  - `get_current_time` -- Current date/time with timezone
  - `set_reminder` -- Create a reminder (Phase 4)
  - `web_search` -- Search the web (Phase 6)

- **`handler.go`** -- HTTP/WebSocket handlers:
  - `POST /api/assistant/chat` -- Synchronous (for simple queries)
  - `POST /api/assistant/chat/stream` -- SSE streaming
  - WebSocket message type `assistant.chat` for bidirectional streaming
  - Return tool calls executed + final response + citations

- **`confirmation.go`** -- Action confirmation workflow:
  - Classify tool calls as safe (read-only) or requires-confirmation (write operations)
  - For write operations, send a confirmation request to the client
  - Client approves/rejects via WebSocket or REST callback
  - Configurable: user can mark certain actions as always-allowed

### 1.2 Multi-Step Reasoning

- Support plan-then-execute flows for complex queries
- LLM generates a plan as a sequence of steps, then executes each
- Example: "Research what I've written about distributed systems and create a synthesis note"
  1. Search notes for "distributed systems"
  2. Read top results
  3. Find related notes via semantic search
  4. Create a synthesis note with findings

### 1.3 Schema & Config Changes

- New config keys:
  - `assistant.max_iterations: 10` -- Max tool-use loop iterations
  - `assistant.confirmation_required: [create_note, update_note, delete_note]` -- Actions needing approval
  - `assistant.model` -- Dedicated model override for assistant (defaults to `models.chat`)

### 1.4 Database Changes

```sql
-- Track assistant actions for audit and undo
CREATE TABLE assistant_actions (
    id          TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    tool_name   TEXT NOT NULL,
    arguments   TEXT NOT NULL,  -- JSON
    result      TEXT,           -- JSON
    status      TEXT NOT NULL DEFAULT 'pending',  -- pending, approved, executed, rejected, failed
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    executed_at DATETIME
);
```

---

## Phase 2: User Profile and Long-Term Memory

**Status**: **Complete** (2026-04-06)

**Goal**: The assistant knows who you are, remembers what you've told it, and improves over time.

**Priority**: High -- without memory, every conversation starts from zero.

### 2.1 User Profile (`internal/assistant/profile.go`)

Structured profile that the assistant can read and update:

- Name, profession, role, organization
- Goals and current focus areas
- Communication preferences (concise vs. detailed, formal vs. casual)
- Topics of interest and expertise
- Timezone and locale
- Custom instructions ("always suggest next actions", "prefer bullet points")

Store as a JSON document in the `settings` table (key: `assistant_profile`) or a dedicated `user_profile` table. Include in the system prompt for every assistant interaction.

### 2.2 Episodic Memory (`internal/assistant/memory.go`)

Long-term memory that persists across conversations:

- After each conversation, extract key facts and decisions
- Store as structured memory entries with categories:
  - **Facts**: "User is working on a Go backend for project X"
  - **Preferences**: "User prefers concise summaries"
  - **Decisions**: "User decided to use PostgreSQL instead of MySQL"
  - **Commitments**: "User said they'd finish the API by Friday"
- Semantic search over memories to include relevant ones in context
- Decay/relevance scoring so stale memories fade
- User can view, edit, and delete memories

### 2.3 Conversation Summarization

- When a conversation exceeds a token threshold, summarize older turns
- Store summary as a memory entry linked to the conversation
- On conversation resume, load summary + recent messages instead of full history
- Removes the current hard limit of 5 turns

### 2.4 Cross-Conversation Retrieval

- Index conversation messages in FTS/ChromaDB
- Support queries like "what did we discuss about X?" across all conversations
- Add a `search_conversations` tool to the assistant

### 2.5 Database Changes

```sql
CREATE TABLE assistant_memories (
    id          TEXT PRIMARY KEY,
    category    TEXT NOT NULL,  -- fact, preference, decision, commitment
    content     TEXT NOT NULL,
    source      TEXT,           -- conversation_id or note_id that spawned this
    confidence  REAL DEFAULT 1.0,
    last_accessed DATETIME,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    expires_at  DATETIME        -- optional TTL for time-bound memories
);

CREATE TABLE user_profile (
    id          TEXT PRIMARY KEY,
    field       TEXT NOT NULL UNIQUE,
    value       TEXT NOT NULL,
    updated_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

---

## Phase 3: Scheduled Triggers and Daily Briefing

**Status**: Not started

**Goal**: The assistant does things without being asked, on a schedule.

**Priority**: High -- proactivity is what separates an assistant from a search engine.

### 3.1 Scheduler Service (`internal/scheduler/`)

- **`scheduler.go`** -- Cron-based task scheduler:
  - Parses cron expressions (use `robfig/cron/v3` or similar)
  - Stores scheduled jobs in SQLite
  - Runs a background goroutine that checks for due jobs
  - Enqueues AI tasks or calls service methods on trigger
  - Supports one-shot (run once at time X) and recurring (cron pattern)

- **`handler.go`** -- CRUD for scheduled jobs:
  - `POST /api/schedules` -- Create schedule
  - `GET /api/schedules` -- List schedules
  - `DELETE /api/schedules/{id}` -- Remove schedule

### 3.2 Daily Briefing

First scheduled job: morning briefing (configurable time, default 08:00 local).

Contents:
- Notes created/modified yesterday
- Open tasks due today or overdue
- Upcoming reminders
- New connections discovered (auto-linker results)
- Suggested actions ("You have 3 orphan notes -- consider linking them")
- Weekly: synthesis of the week's activity

Delivery: create a daily briefing note in a `briefings` project, push via WebSocket, optionally email.

### 3.3 Database Changes

```sql
CREATE TABLE schedules (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    cron_expr   TEXT,           -- cron expression for recurring
    run_at      DATETIME,       -- for one-shot schedules
    action_type TEXT NOT NULL,  -- briefing, automation, reminder
    action_config TEXT NOT NULL, -- JSON payload
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    last_run_at DATETIME,
    next_run_at DATETIME,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

---

## Phase 4: Reminders and Due Dates

**Goal**: The assistant tracks deadlines and nudges you at the right time.

**Priority**: High -- time awareness is fundamental to a personal assistant.

### 4.1 Task Enhancements

Extend the existing `tasks` table:

```sql
ALTER TABLE tasks ADD COLUMN priority    INTEGER DEFAULT 0;   -- 0=none, 1=low, 2=medium, 3=high, 4=urgent
ALTER TABLE tasks ADD COLUMN due_at      DATETIME;
ALTER TABLE tasks ADD COLUMN remind_at   DATETIME;
ALTER TABLE tasks ADD COLUMN recurrence  TEXT;                 -- cron or RRULE for recurring tasks
ALTER TABLE tasks ADD COLUMN completed_at DATETIME;
```

### 4.2 Reminder Service (`internal/reminder/`)

- Check for due reminders on a ticker (every minute)
- Push reminder notifications via WebSocket
- Snooze support (remind again in 15m/1h/tomorrow)
- Overdue task escalation (daily nag for overdue items)
- Natural language parsing: "remind me about X tomorrow at 3pm"

### 4.3 Calendar/Time Awareness

- The assistant always knows the current date, time, day of week
- Can reason about relative time ("last week", "next Monday", "in 3 days")
- Tasks and reminders surfaced in daily briefing based on temporal relevance

---

## Phase 5: Event-Driven Automations

**Goal**: Define rules that trigger AI actions when things happen in Seam.

**Priority**: Medium -- builds on the agentic loop and scheduler.

### 5.1 Automation Engine (`internal/automations/`)

- **`automation.go`** -- Rule definition:
  - Trigger: event type + optional filter (tag, project, title pattern)
  - Action: AI task type + parameters
  - Examples:
    - "When a note tagged #meeting is created, extract action items and create tasks"
    - "When a note is moved to the 'archive' project, generate a summary"
    - "When I capture a URL, extract key quotes and tag appropriately"

- **`engine.go`** -- Event listener:
  - Subscribes to internal events (note.created, note.modified, note.deleted, task.completed)
  - Evaluates automation rules against events
  - Enqueues matching actions in the AI task queue

- **`handler.go`** -- CRUD for automations:
  - `POST /api/automations`
  - `GET /api/automations`
  - `PUT /api/automations/{id}`
  - `DELETE /api/automations/{id}`

### 5.2 Built-in Automations (Defaults)

Ship with sensible defaults that users can enable:

| Trigger | Action |
|---|---|
| Note created with #meeting tag | Extract action items as tasks |
| Note created with #article tag | Generate summary + key takeaways |
| URL captured | Extract title, key quotes, suggest tags |
| Voice capture transcribed | Summarize transcript, extract tasks |
| Task overdue by 3 days | Add to daily briefing with escalation |

### 5.3 Database Changes

```sql
CREATE TABLE automations (
    id          TEXT PRIMARY KEY,
    name        TEXT NOT NULL,
    trigger_event TEXT NOT NULL,  -- note.created, note.modified, task.completed, etc.
    trigger_filter TEXT,          -- JSON: {tag: "meeting", project: "work"}
    action_type TEXT NOT NULL,    -- extract_tasks, summarize, suggest_tags, custom_prompt
    action_config TEXT NOT NULL,  -- JSON: {prompt: "...", target_project: "..."}
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    run_count   INTEGER NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

---

## Phase 6: Document Ingestion

**Goal**: Expand the knowledge base beyond markdown files.

**Priority**: Medium -- significantly increases the assistant's usefulness.

### 6.1 Ingestion Pipeline (`internal/ingest/`)

- **`ingest.go`** -- Common interface:
  - Accept file path or byte stream + MIME type
  - Extract plain text content
  - Create a Seam note with extracted content + source metadata in frontmatter
  - Trigger embedding pipeline

- **`pdf.go`** -- PDF text extraction (use `pdfcpu` or `unipdf`, pure Go)
- **`html.go`** -- HTML to markdown conversion (enhance existing URL capture)
- **`epub.go`** -- EPUB extraction
- **`plaintext.go`** -- TXT, CSV, log files
- **`image.go`** -- OCR via Ollama vision models (llava, etc.)

### 6.2 Watch Folders

- Configure directories to watch for new files (e.g., `~/Downloads/*.pdf`)
- Auto-ingest on file creation
- Track ingested files to avoid duplicates (store hash + source path)

### 6.3 API

- `POST /api/ingest` -- Upload file for ingestion (multipart)
- `POST /api/ingest/batch` -- Ingest multiple files
- `GET /api/ingest/status` -- Check ingestion queue status

---

## Phase 7: External Integrations

**Goal**: Connect Seam to the services you already use.

**Priority**: Medium -- depends on user needs, implement selectively.

### 7.1 Integration Framework (`internal/integrations/`)

- **`integration.go`** -- Common interface:
  - `Sync(ctx) error` -- Pull new data
  - `Push(ctx, item) error` -- Send data out (optional)
  - OAuth2 token management for services requiring it
  - Configurable sync intervals

### 7.2 Calendar Integration

- **CalDAV / iCal import**: Read `.ics` files or subscribe to CalDAV feeds
- Surface today's events in daily briefing
- "What's on my calendar this week?" via assistant tool
- Create notes from calendar events (meeting prep, meeting notes template)

### 7.3 Email Integration

- **IMAP reader**: Connect to email account (read-only)
- AI summarizes unread emails
- Create notes from important emails
- "What emails need my attention?" via assistant tool
- No email sending (privacy boundary -- assistant is read-only for email)

### 7.4 RSS/Feed Reader

- Subscribe to RSS/Atom feeds
- AI summarizes new articles, creates notes for interesting ones
- Tag and categorize automatically

### 7.5 External Task Sync

- Bidirectional sync with Todoist, Linear, GitHub Issues
- Seam tasks as the unified view, synced to/from external systems

---

## Phase 8: Notifications and Communication

**Goal**: The assistant can reach you when it has something to say.

**Priority**: Medium-Low -- WebSocket covers the in-app case; this extends reach.

### 8.1 Notification Service (`internal/notify/`)

- **`notify.go`** -- Unified notification dispatch:
  - In-app (WebSocket push) -- already exists
  - Desktop notifications (via system tray app or Electron wrapper)
  - Email digest (daily/weekly summary email)
  - Optional: ntfy.sh, Pushover, or Gotify for mobile push

- **`preferences.go`** -- User notification preferences:
  - Which events trigger notifications
  - Quiet hours (no notifications between 10pm-8am)
  - Delivery channel per event type
  - Urgency thresholds

### 8.2 Notification Queue

- Store pending notifications in SQLite
- Batch delivery for digest channels (email)
- Deduplication (don't notify about the same thing twice)
- Read/unread tracking for in-app notifications

### 8.3 Database Changes

```sql
CREATE TABLE notifications (
    id          TEXT PRIMARY KEY,
    type        TEXT NOT NULL,     -- reminder, briefing, suggestion, alert
    title       TEXT NOT NULL,
    body        TEXT NOT NULL,
    source_type TEXT,              -- note, task, automation, schedule
    source_id   TEXT,
    priority    INTEGER NOT NULL DEFAULT 0,
    read        BOOLEAN NOT NULL DEFAULT 0,
    delivered   BOOLEAN NOT NULL DEFAULT 0,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    read_at     DATETIME
);

CREATE TABLE notification_preferences (
    id          TEXT PRIMARY KEY,
    event_type  TEXT NOT NULL UNIQUE,
    channels    TEXT NOT NULL,     -- JSON array: ["websocket", "email"]
    enabled     BOOLEAN NOT NULL DEFAULT 1,
    quiet_start TEXT,              -- "22:00"
    quiet_end   TEXT               -- "08:00"
);
```

---

## Phase 9: Planning and Goal Tracking

**Goal**: Elevate from task checkboxes to structured goal management.

**Priority**: Lower -- valuable but not core to the assistant identity.

### 9.1 Goals (`internal/goals/`)

- Hierarchical: Goals > Milestones > Tasks
- Each goal has: title, description, target date, status, linked project
- Progress computed from child milestone/task completion
- AI suggests next actions toward goals based on recent activity
- Weekly goal review prompt from the assistant

### 9.2 Smart Prioritization

- Eisenhower matrix (urgent/important) classification
- AI suggests priority based on due date, goal alignment, and context
- "What should I work on next?" as a first-class assistant capability
- Energy-aware scheduling: tag tasks as deep-work vs. shallow, suggest based on time of day

### 9.3 Database Changes

```sql
CREATE TABLE goals (
    id          TEXT PRIMARY KEY,
    title       TEXT NOT NULL,
    description TEXT,
    project_id  TEXT REFERENCES projects(id),
    status      TEXT NOT NULL DEFAULT 'active',  -- active, achieved, abandoned
    target_date DATETIME,
    created_at  DATETIME NOT NULL DEFAULT (datetime('now')),
    achieved_at DATETIME
);

CREATE TABLE milestones (
    id          TEXT PRIMARY KEY,
    goal_id     TEXT NOT NULL REFERENCES goals(id),
    title       TEXT NOT NULL,
    status      TEXT NOT NULL DEFAULT 'pending',
    target_date DATETIME,
    completed_at DATETIME
);

-- Link tasks to milestones/goals
ALTER TABLE tasks ADD COLUMN milestone_id TEXT REFERENCES milestones(id);
ALTER TABLE tasks ADD COLUMN goal_id TEXT REFERENCES goals(id);
```

---

## Phase 10: Learning and Personalization

**Goal**: The assistant gets better the more you use it.

**Priority**: Lower -- polish layer on top of working assistant.

### 10.1 Feedback Loop

- Thumbs up/down on assistant responses
- Store feedback linked to conversation + response
- Use feedback to tune system prompts (few-shot examples of good responses)
- Periodic self-evaluation: "Here are responses you liked/disliked -- adjusting approach"

### 10.2 Topic Interest Model

- Track which notes/topics the user engages with most (views, edits, searches)
- Build a weighted interest profile
- Prioritize related content in search results and suggestions
- Surface "you haven't revisited X in a while, it might be relevant to Y"

### 10.3 Smart Suggestions

Beyond tag/project suggestions (which already exist):

- "These 3 notes seem related -- should I link them?"
- "You've written a lot about X -- want me to create a synthesis?"
- "This note contradicts what you wrote in [other note] -- worth reviewing?"
- "Based on your recent notes, you might find [topic] interesting"

### 10.4 Database Changes

```sql
CREATE TABLE assistant_feedback (
    id              TEXT PRIMARY KEY,
    conversation_id TEXT NOT NULL REFERENCES conversations(id),
    message_index   INTEGER NOT NULL,
    rating          INTEGER NOT NULL,  -- -1 (bad), 0 (neutral), 1 (good)
    comment         TEXT,
    created_at      DATETIME NOT NULL DEFAULT (datetime('now'))
);

CREATE TABLE topic_engagement (
    id          TEXT PRIMARY KEY,
    topic       TEXT NOT NULL,
    note_id     TEXT REFERENCES notes(id),
    action      TEXT NOT NULL,  -- view, edit, search, link
    created_at  DATETIME NOT NULL DEFAULT (datetime('now'))
);
```

---

## Implementation Order Summary

| Phase | Name | Effort | Depends On |
|---|---|---|---|
| 1 | Agentic loop with tool use | Large | -- |
| 2 | User profile + long-term memory | Medium | Phase 1 |
| 3 | Scheduled triggers + daily briefing | Medium | Phase 1 |
| 4 | Reminders + due dates | Small-Medium | Phase 3 |
| 5 | Event-driven automations | Medium | Phase 1, Phase 3 |
| 6 | Document ingestion | Medium | -- (independent) |
| 7 | External integrations | Large | Phase 3 |
| 8 | Notifications | Medium | Phase 3, Phase 4 |
| 9 | Planning + goal tracking | Medium | Phase 4 |
| 10 | Learning + personalization | Medium | Phase 1, Phase 2 |

Phase 1 is the critical path. Nearly everything else depends on the assistant being able to use tools and take actions. Phase 6 (document ingestion) is independent and can be built in parallel.

---

## Architecture Principles

1. **Same layering**: All new packages follow `handler.go` / `service.go` / `store.go`. No shortcuts.
2. **Assistant actions are auditable**: Every tool call the assistant makes is logged in `assistant_actions`.
3. **User stays in control**: Write operations require confirmation by default. Users can whitelist specific actions.
4. **Local-first**: External integrations are optional. The assistant works fully offline with Ollama.
5. **Incremental**: Each phase delivers standalone value. No phase requires completing all subsequent phases.
6. **No new infrastructure**: Everything runs in the single `seamd` process with SQLite. No Redis, no Postgres, no message queues beyond the existing in-process AI task queue.
