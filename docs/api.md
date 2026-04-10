# API Reference

## REST Endpoints

### Auth

```
POST   /api/auth/register
POST   /api/auth/login
POST   /api/auth/refresh
POST   /api/auth/logout
GET    /api/auth/me
PUT    /api/auth/password
PUT    /api/auth/email
```

### Notes

```
POST   /api/notes                     # create (supports template field)
GET    /api/notes                     # list (filter by project, tag, date; paginated)
GET    /api/notes/daily               # get/create daily note (?date=YYYY-MM-DD)
PATCH  /api/notes/bulk                # bulk actions
GET    /api/notes/{id}
PUT    /api/notes/{id}
DELETE /api/notes/{id}
POST   /api/notes/{id}/append         # append content
GET    /api/notes/{id}/backlinks
GET    /api/notes/resolve              # resolve wikilink to note ID
GET    /api/notes/{id}/versions        # version history
GET    /api/notes/{id}/versions/{v}    # specific version
POST   /api/notes/{id}/versions/{v}/restore
```

### Projects

```
GET    /api/projects
POST   /api/projects
GET    /api/projects/{id}
PUT    /api/projects/{id}
DELETE /api/projects/{id}
```

### Search

```
GET    /api/search?q=...              # full-text (FTS5, recency_bias param)
GET    /api/search/semantic?q=...     # semantic (embeddings, recency_bias param)
```

### AI

```
POST   /api/ai/ask                    # RAG chat (query + history -> response + citations)
POST   /api/ai/synthesize             # cross-note synthesis
POST   /api/ai/synthesize/stream      # streaming synthesis (SSE)
POST   /api/ai/reindex-embeddings     # bulk reindex all embeddings
GET    /api/ai/notes/{id}/related     # semantically similar notes
POST   /api/ai/notes/{id}/assist      # writing assist (expand/summarize/extract-actions)
POST   /api/ai/suggest-tags           # AI tag suggestions
POST   /api/ai/suggest-project        # AI project suggestions
```

### Assistant

The agentic assistant. Persistent profile and long-term memory live alongside chat. Mutating tools (`create_note`, `update_note`, `append_to_note`, `create_project`, `save_memory`, `update_profile`) pause the loop and require explicit approval; the client resumes via `/actions/{id}/approve` or `/actions/{id}/resume`. See [docs/ai.md](ai.md#assistant) for the full feature description and [docs/security.md](security.md#assistant-safety) for the confirmation gating model.

```
POST   /api/assistant/chat                                   # one-shot agentic chat
POST   /api/assistant/chat/stream                            # streaming agentic chat (SSE)
GET    /api/assistant/conversations/{conversationID}/actions # audit log of executed tool calls
POST   /api/assistant/actions/{actionID}/approve             # approve a pending action (no resume)
POST   /api/assistant/actions/{actionID}/resume              # approve and resume the agent loop (SSE)
POST   /api/assistant/actions/{actionID}/reject              # reject a pending action

GET    /api/assistant/profile                                # owner profile (instructions, facts)
PUT    /api/assistant/profile                                # update profile

GET    /api/assistant/memories                               # list long-term memories
POST   /api/assistant/memories                               # create a memory
DELETE /api/assistant/memories/{memoryID}
```

### Schedules

Cron-based proactive jobs. The default daily briefing is auto-provisioned on first start (configurable via `seam-server.yaml` -> `scheduler.daily_briefing`).

```
POST   /api/schedules                 # create
GET    /api/schedules                 # list
GET    /api/schedules/{id}
PUT    /api/schedules/{id}
DELETE /api/schedules/{id}
POST   /api/schedules/{id}/run        # run now (out of band)
```

### Capture

```
POST   /api/capture                   # quick capture (URL or voice)
```

### Templates

```
GET    /api/templates
GET    /api/templates/{name}
POST   /api/templates/{name}/apply
```

### Graph

```
GET    /api/graph                     # nodes + edges (filter by project/tag/date)
GET    /api/graph/two-hop-backlinks/{id}
GET    /api/graph/orphans
```

### Tags

```
GET    /api/tags                      # all tags with note counts
```

### Tasks

```
GET    /api/tasks                     # checkbox items from notes
GET    /api/tasks/summary             # aggregate counts
GET    /api/tasks/{id}
PATCH  /api/tasks/{id}               # toggle done
```

### Chat History

```
POST   /api/chat/conversations
GET    /api/chat/conversations
GET    /api/chat/conversations/{id}
DELETE /api/chat/conversations/{id}
POST   /api/chat/conversations/{id}/messages
```

### Review

```
GET    /api/review/queue              # knowledge gardening queue
```

### Settings

```
GET    /api/settings
PUT    /api/settings
DELETE /api/settings/{key}
```

### Token Usage

Dashboard for AI token consumption. Tracks every LLM and embedding call by function, provider, and model. Optional budget enforcement via settings.

```
GET    /api/usage/summary                # aggregated totals (from/to query params, default last 30 days)
GET    /api/usage/by-function            # breakdown by function (chat, assistant, embedding, etc.)
GET    /api/usage/by-provider            # breakdown by provider (ollama, openai, anthropic)
GET    /api/usage/by-model               # breakdown by model (gpt-4o, gpt-4o-mini, llama3, etc.)
GET    /api/usage/timeseries             # time series (granularity: hour/day/month)
GET    /api/usage/budget                 # current budget status
PUT    /api/usage/budget                 # update budget settings
```

### Webhooks

```
POST   /api/webhooks                  # create (returns HMAC secret)
GET    /api/webhooks
GET    /api/webhooks/events           # subscribable event types
GET    /api/webhooks/{id}
PUT    /api/webhooks/{id}
DELETE /api/webhooks/{id}
GET    /api/webhooks/{id}/deliveries  # delivery history
```

### Health

```
GET    /api/health
```

### MCP

```
POST   /api/mcp                       # Streamable HTTP (Model Context Protocol)
```

## WebSocket

```
/api/ws                                 # authenticated connection per user
```

Events: `note.changed`, `task.progress`, `task.complete`, `task.failed`, `chat.stream`, `chat.done`, `note.link_suggestions`, `webhook.delivery`
