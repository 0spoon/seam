# MCP Agent Memory

Seam exposes an [MCP (Model Context Protocol)](https://modelcontextprotocol.io/) server at `/api/mcp`, giving AI coding agents persistent long-term memory. Your agents can track sessions, store knowledge, search your notes, create new ones, manage tasks, and register webhooks -- all through standard MCP tools.

## Connecting

Any MCP-compatible client (Claude Code, Cursor, etc.) connects via Streamable HTTP:

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

| Tool | What it does |
|---|---|
| `session_start` | Start or resume a named session. Returns a briefing with context |
| `session_plan_set` | Set the session plan |
| `session_progress_update` | Log task progress (pending/in_progress/completed/blocked) |
| `session_context_set` | Set session context notes |
| `session_end` | End session with findings summary |
| `session_list` | List sessions by status |
| `session_metrics` | Aggregate stats (tool calls, durations, errors) |

### Agent Memory

Persistent knowledge that survives across sessions. Your agent's long-term memory.

| Tool | What it does |
|---|---|
| `memory_write` | Create or update a knowledge note by category and name |
| `memory_read` | Read a knowledge note |
| `memory_append` | Append to an existing note |
| `memory_list` | List notes, optionally by category |
| `memory_delete` | Delete a knowledge note |
| `memory_search` | FTS + semantic search scoped to agent memory |

### User Notes

Read, search, and create user-facing notes.

| Tool | What it does |
|---|---|
| `notes_search` | Full-text search with recency bias |
| `notes_read` | Read a note by ID |
| `notes_list` | List notes with project/tag filtering |
| `notes_create` | Create a user note (auto-tagged `created-by:agent`) |

### Tasks & Webhooks

Track tasks from your notes and register HTTP callbacks for events.

| Tool | What it does |
|---|---|
| `tasks_list` | List checkbox tasks from notes |
| `tasks_summary` | Aggregate task counts |
| `context_gather` | Budgeted search across notes with ranked snippets |
| `webhook_register` | Register webhook for note/task events |
| `webhook_list` | List registered webhooks |
| `webhook_delete` | Delete a webhook |

## Rate Limits

60 requests/minute per user.
