# MCP Server

You work with many agents. A Claude Code session for backend work, a Cursor session for frontend, a research conversation for architecture decisions. Each one is a separate context window. Each one forgets everything when the conversation ends. The insight from your morning session doesn't exist in your afternoon session. What one agent discovered, the other will never know. The knowledge that didn't make it into code -- the reasoning, the dead ends, the decisions -- is lost.

Most "agent memory" solutions offer a key-value store with vector search. Seam is different: it exposes a full knowledge system -- notes, projects, tasks, a knowledge graph, daily briefings -- as an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server at `/api/mcp`. Your agents don't just store and retrieve memories. They operate inside the same workspace you do.

**What agents can do inside Seam:**

- **Start a session** and get a briefing: what happened in past sessions, what other agents found, open tasks, relevant notes.
- **Search, read, create, and organize notes** -- the same `.md` files you work with. Not a parallel memory store, but the actual knowledge base.
- **Manage projects and tasks** -- create projects, track checkbox tasks from notes, check what's on the plate.
- **Traverse the knowledge graph** -- follow wikilinks, backlinks, and two-hop connections to find related context.
- **Collaborate** on the same problem through session hierarchies and shared [research labs](#research-lab). Agent B sees what Agent A tried and decided.
- **Record findings** that persist after the conversation ends, so the next session picks up where this one left off.

## Connecting

Any MCP-compatible client connects via Streamable HTTP:

```json
{
  "mcpServers": {
    "seam": {
      "url": "http://localhost:8080/api/mcp",
      "headers": {
        "Authorization": "Bearer <jwt_access_token>"
      }
    }
  }
}
```

## Available Tools

### Session Management

Track agent working sessions with plans, progress, and findings. Sessions form hierarchies (subagents see parent plans and sibling findings).

| Tool                      | What it does                                                     |
| ------------------------- | ---------------------------------------------------------------- |
| `session_start`           | Start or resume a named session. Returns a briefing with context |
| `session_plan_set`        | Set the session plan                                             |
| `session_progress_update` | Log task progress (pending/in_progress/completed/blocked)        |
| `session_context_set`     | Set session context notes                                        |
| `session_end`             | End session with findings summary                                |
| `session_list`            | List sessions by status                                          |
| `session_metrics`         | Aggregate stats (tool calls, durations, errors)                  |

### Agent Memory

Persistent knowledge that survives across sessions. Your agent's long-term memory.

| Tool            | What it does                                           |
| --------------- | ------------------------------------------------------ |
| `memory_write`  | Create or update a knowledge note by category and name |
| `memory_read`   | Read a knowledge note                                  |
| `memory_append` | Append to an existing note                             |
| `memory_list`   | List notes, optionally by category                     |
| `memory_delete` | Delete a knowledge note                                |
| `memory_search` | FTS + semantic search scoped to agent memory           |

### User Notes

Read, search, create, update, and delete user-facing notes.

| Tool           | What it does                                         |
| -------------- | ---------------------------------------------------- |
| `notes_search` | Full-text search with recency bias                   |
| `notes_read`   | Read a note by ID                                    |
| `notes_list`   | List notes with project/tag filtering                |
| `notes_create` | Create a user note (auto-tagged `created-by:agent`)  |
| `notes_update` | Update a note's title, body, tags, or project        |
| `notes_delete` | Permanently delete a note by ID                      |
| `notes_tags`   | List all tags in use with counts                     |
| `notes_daily`  | Get or create today's daily note                     |
| `notes_append` | Append timestamped text to a note (log-style)        |
| `notes_changelog` | List notes changed within a date range            |
| `notes_versions` | List version history or retrieve a past version    |
| `notes_from_template` | Create a note from a named template with variables |

### Knowledge Graph

Explore structural relationships between notes.

| Tool               | What it does                                             |
| ------------------ | -------------------------------------------------------- |
| `graph_neighbors`  | Backlinks, two-hop connections for a note                |

### Knowledge Gardening

Autonomous knowledge base maintenance.

| Tool           | What it does                                                   |
| -------------- | -------------------------------------------------------------- |
| `review_queue` | Pull items needing attention (orphans, untagged, unsorted)     |

### Projects

Manage note organization.

| Tool             | What it does                |
| ---------------- | --------------------------- |
| `project_list`   | List all projects           |
| `project_create` | Create a new project        |

### Tasks & Webhooks

Track tasks from your notes and register HTTP callbacks for events.

| Tool               | What it does                                      |
| ------------------ | ------------------------------------------------- |
| `tasks_list`       | List checkbox tasks from notes                    |
| `tasks_summary`    | Aggregate task counts                             |
| `tasks_toggle`     | Toggle a task's done status                       |
| `context_gather`   | Budgeted search across notes with ranked snippets |
| `webhook_register` | Register webhook for note/task events             |
| `webhook_list`     | List registered webhooks                          |
| `webhook_delete`   | Delete a webhook                                  |

### Research Lab

Systematic debugging and experimentation tracking. Multiple agents can collaborate on the same investigation via the session hierarchy.

| Tool              | What it does                                                                          |
| ----------------- | ------------------------------------------------------------------------------------- |
| `lab_open`        | Open or resume a research lab. Returns briefing, notebook, and past trial summaries   |
| `trial_record`    | Record a trial (changes + expected). Update later with actual + outcome               |
| `decision_record` | Record a decision based on accumulated evidence from trials                           |
| `trial_query`     | Search and list trials in a lab, filtered by outcome or text query                    |

**Workflow:** `lab_open` -> `trial_record` (changes + expected) -> observe -> `trial_record` (actual + outcome) -> `decision_record` when evidence accumulates.

**Multi-agent:** Multiple agents call `lab_open` with the same name. Each agent's completed trial findings appear in other agents' briefings via session hierarchy (`lab/{name}/{trial-slug}`).

**Outcomes:** `success`, `failure`, `partial`, `inconclusive`, `pending` (default until set).

## Rate Limits

60 requests/minute per user.
