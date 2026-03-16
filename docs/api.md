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
