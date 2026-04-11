# AI & Assistant

Seam's AI layer works both sides: it helps *you* think, search, and write inside your knowledge base, and it gives your *agents* a structured workspace to operate in across sessions -- not just a memory store, but the same notes, projects, and tasks you work with. Everything runs locally by default through Ollama. No API keys, no usage meters, no one reading your notes. Switch providers with one config line when you need more power.

## Providers

Three LLM providers, mixable. Embeddings run locally by default (your vectors stay on your machine), while chat completions can come from wherever you want:

| Provider | Config | Good for |
| --- | --- | --- |
| **Ollama** (default) | `llm.provider: "ollama"` | Privacy maximalists, people with beefy GPUs, not paying per token |
| **OpenAI** | `llm.provider: "openai"` | GPT-4o, or any OpenAI-compatible API (Azure, Together, Groq, etc.) |
| **Anthropic** | `llm.provider: "anthropic"` | Claude |

Switch providers with one config line. Mix and match -- local embeddings with cloud chat is the sweet spot for most setups.

## Features

**Ask Seam** -- Chat with your notes. Ask a question, get an answer grounded in things you actually wrote, with citations. It embeds your query, retrieves relevant chunks from ChromaDB, and streams a response with full conversation history.

**Semantic Search** -- "What did I write about caching strategies?" works even if you never used the word "caching." Embedding-based similarity search with optional recency bias.

**Synthesis** -- "Summarize everything I know about project X." Seam pulls up to 50 relevant notes and generates a cross-note synthesis. Available as a regular response or SSE streaming.

**Auto-Link Suggestions** -- On save, Seam reads your note, finds semantically similar content, and suggests wikilinks you probably should have added.

**Writing Assist** -- Select text and ask AI to expand a bullet into a paragraph, summarize a wall of text, or extract action items into a checklist. Three modes: `expand`, `summarize`, `extract-actions`.

**Tag & Project Suggestions** -- AI reads your note content and suggests tags from your existing taxonomy and which project it belongs to.

**Related Notes** -- Every note shows semantically similar notes in a sidebar. The connections you did not know you had.

**Voice Transcription** -- Record audio, Whisper transcribes locally, AI auto-summarizes. No audio leaves your machine.

## Task Queue

All AI work runs through a priority queue with fair round-robin scheduling across users. Interactive requests (chat, writing assist) jump the line. Background tasks (embeddings, auto-linking) wait politely. Tasks survive server restarts. Configurable workers, timeouts, and retries.

## Assistant

The agentic assistant is a tool-use loop that can actually do things in your knowledge base, not just answer questions about it. You ask it to capture something, plan a project, find connections, or rewrite a section, and it calls into your notes, projects, tasks, search, graph, profile, and long-term memory until the request is satisfied or it hits the iteration cap.

**How it runs.** Each user message kicks off `runAgentLoop`. The model picks a tool, the loop executes it, the result goes back to the model, and the cycle repeats up to `MaxIterations` (default 10). Streaming is over SSE so you see each tool call land in real time. Conversation history is persisted so you can resume.

**Confirmation gating.** Six tools that mutate persistent state pause the loop and surface a confirmation event to the client: `create_note`, `update_note`, `append_to_note`, `create_project`, `save_memory`, and `update_profile`. Nothing is written until you click Approve. The client resumes the agent loop via `POST /api/assistant/actions/{id}/resume`. The list is configurable in `seam-server.yaml` under `assistant.confirmation_required`, but `save_memory` and `update_profile` are load-bearing -- both feed back into a future system prompt, so removing them re-opens a persistent prompt-injection escalation path. See [docs/security.md](security.md#assistant-safety).

**Profile and long-term memory.** A persistent profile (instructions plus free-form facts) and a separate long-term memory store (categorised facts, preferences, decisions, commitments) are loaded into the system prompt every turn. Both are searchable via FTS5 with recency decay. The assistant can write to both via the gated `update_profile` and `save_memory` tools, and the user manages them via `/api/assistant/profile` and `/api/assistant/memories`.

**Audit trail.** Every tool call is recorded in `assistant_actions` with the arguments and result. `recordAction` only sets `executed_at` on success, so failed actions are visible in the audit log too. Available via `GET /api/assistant/conversations/{id}/actions`.

### Tools

19 tools, grouped by capability. The "Mutates" column flags the six that require confirmation.

| Tool                   | Mutates | What it does                                        |
| ---------------------- | ------- | --------------------------------------------------- |
| `search_notes`         |         | FTS5 search across notes with optional recency bias |
| `read_note`            |         | Read a note by ID                                   |
| `list_notes`           |         | List notes with project/tag filters                 |
| `create_note`          | yes     | Create a new note (title, body, project, tags)      |
| `update_note`          | yes     | Replace a note's body and metadata                  |
| `append_to_note`       | yes     | Append content to an existing note                  |
| `list_projects`        |         | List all projects                                   |
| `create_project`       | yes     | Create a new project                                |
| `list_tasks`           |         | List checkbox tasks from notes                      |
| `toggle_task`          |         | Toggle a checkbox task done/undone                  |
| `get_daily_note`       |         | Get or create today's daily note                    |
| `get_graph`            |         | Knowledge graph (nodes + edges)                     |
| `find_related`         |         | Semantically similar notes for a given note ID      |
| `get_current_time`     |         | Server-local current time                           |
| `search_conversations` |         | Search prior assistant conversation history         |
| `save_memory`          | yes     | Write a long-term memory entry                      |
| `search_memories`      |         | Search long-term memories                           |
| `get_profile`          |         | Read the owner profile                              |
| `update_profile`       | yes     | Update the owner profile (instructions, facts)      |

## Librarian

The librarian is an autonomous background service that organizes your knowledge base. Think of it as a library that automatically shelves returned books: agents and users dump notes into the inbox, and the librarian periodically reviews them, assigns projects, and adds tags.

**How it works.** The librarian runs as a scheduler action (every 10 minutes by default). Each sweep:

1. Checks the `librarian_enabled` setting -- exits immediately if disabled.
2. Pulls candidates from the review queue (orphan, untagged, inbox notes).
3. Fetches all existing projects and tags as classification context.
4. For each candidate (up to `max_per_run`, default 10):
   - Skips notes updated within the cooldown window (default 15 minutes).
   - Skips notes already tagged `librarian:reviewed`.
   - Sends the note title and body to the LLM with the list of available projects and tags.
   - Verifies the note's content hash has not changed during the LLM call (protects against concurrent edits).
   - Applies the classification: assigns a project (only if the note has none) and merges suggested tags.
   - Tags the note `librarian:reviewed` so it is not reprocessed.
5. Sends a `librarian.action` WebSocket message for each note it organizes.

**Safety constraints:**

- Never modifies note content, titles, or creates new projects/tags -- only uses what already exists.
- Only processes notes that have been quiet for 15+ minutes (configurable cooldown).
- Content hash guard: re-reads the note after the LLM call and aborts if anything changed.
- Never overwrites an existing project assignment.
- Defaults to disabled -- must be explicitly turned on.

**Enabling.** Toggle via the settings API:

```bash
curl -X PUT http://localhost:8080/api/settings \
  -H "Authorization: Bearer <token>" \
  -H "Content-Type: application/json" \
  -d '{"librarian_enabled": "true"}'
```

**Configuration.** The schedule's `action_config` JSON supports:

| Field | Default | Description |
| --- | --- | --- |
| `cooldown_minutes` | 15 | Minimum quiet time before a note is eligible |
| `max_per_run` | 10 | Maximum notes processed per sweep |

**Requirements.** The librarian requires an LLM provider to be configured (`llm.provider` in `seam-server.yaml`). It uses the `models.background` model for classification. If no LLM is configured, the librarian is not registered.

## Default Models

| Role             | Default Model        | Swappable?                                |
| ---------------- | -------------------- | ----------------------------------------- |
| Embeddings       | `qwen3-embedding:8b` | Yes, any Ollama or OpenAI embedding model |
| Chat             | `qwen3:32b`          | Yes, or use OpenAI/Anthropic              |
| Background tasks | `qwen3:32b`          | Yes, or use OpenAI/Anthropic              |

Switching the embedding provider or model invalidates the existing Chroma collection (each `(provider, model)` tuple gets its own collection name). After a switch, run `make reindex` (or `./bin/seam-reindex`) to repopulate.
