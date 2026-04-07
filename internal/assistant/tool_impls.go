package assistant

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/katata/seam/internal/chat"
	"github.com/katata/seam/internal/graph"
	"github.com/katata/seam/internal/note"
	"github.com/katata/seam/internal/project"
	"github.com/katata/seam/internal/search"
	"github.com/katata/seam/internal/task"
)

// RegisterDefaultTools registers all built-in tools that delegate to domain services.
func RegisterDefaultTools(
	registry *ToolRegistry,
	noteSvc *note.Service,
	taskSvc *task.Service,
	projectSvc *project.Service,
	searchSvc *search.Service,
	graphSvc *graph.Service,
	chatSvc *chat.Service,
) {
	registerSearchNotes(registry, searchSvc)
	registerReadNote(registry, noteSvc)
	registerCreateNote(registry, noteSvc)
	registerUpdateNote(registry, noteSvc)
	registerAppendToNote(registry, noteSvc)
	registerListNotes(registry, noteSvc)
	registerListProjects(registry, projectSvc)
	registerCreateProject(registry, projectSvc)
	registerListTasks(registry, taskSvc)
	registerToggleTask(registry, taskSvc)
	registerGetDailyNote(registry, noteSvc)
	registerGetGraph(registry, graphSvc)
	registerFindRelated(registry, searchSvc)
	registerGetCurrentTime(registry)
	if chatSvc != nil {
		registerSearchConversations(registry, chatSvc)
	}
}

// RegisterMemoryTools registers tools for the assistant's long-term memory
// and user profile management.
func RegisterMemoryTools(registry *ToolRegistry, svc *Service) {
	registerSaveMemory(registry, svc)
	registerSearchMemories(registry, svc)
	registerGetProfile(registry, svc)
	registerUpdateProfile(registry, svc)
}

func registerSearchNotes(registry *ToolRegistry, searchSvc *search.Service) {
	registry.Register(&Tool{
		Name:        "search_notes",
		Description: "Search the user's notes using full-text search. Returns matching notes with snippets.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum number of results (default 10, max 50)",
				},
			},
			"required": []string{"query"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 || params.Limit > 50 {
				params.Limit = 10
			}
			results, total, err := searchSvc.SearchFTS(ctx, userID, params.Query, params.Limit, 0)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"results": results,
				"total":   total,
			})
		},
		ReadOnly: true,
	})
}

func registerReadNote(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "read_note",
		Description: "Get the full content of a note by its ID.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"note_id": map[string]interface{}{
					"type":        "string",
					"description": "The note ID (ULID)",
				},
			},
			"required": []string{"note_id"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				NoteID string `json:"note_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			n, err := noteSvc.Get(ctx, userID, params.NoteID)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":         n.ID,
				"title":      n.Title,
				"body":       n.Body,
				"tags":       n.Tags,
				"project_id": n.ProjectID,
				"created_at": n.CreatedAt.Format(time.RFC3339),
				"updated_at": n.UpdatedAt.Format(time.RFC3339),
			})
		},
		ReadOnly: true,
	})
}

func registerCreateNote(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "create_note",
		Description: "Create a new note with a title, body, optional tags, and optional project ID.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"title": map[string]interface{}{
					"type":        "string",
					"description": "The note title",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "The note body in Markdown",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "Tags for the note",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Optional project ID to place the note in",
				},
			},
			"required": []string{"title", "body"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Title     string   `json:"title"`
				Body      string   `json:"body"`
				Tags      []string `json:"tags"`
				ProjectID string   `json:"project_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			n, err := noteSvc.Create(ctx, userID, note.CreateNoteReq{
				Title:     params.Title,
				Body:      params.Body,
				Tags:      params.Tags,
				ProjectID: params.ProjectID,
			})
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":    n.ID,
				"title": n.Title,
			})
		},
		ReadOnly: false,
	})
}

func registerUpdateNote(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "update_note",
		Description: "Update an existing note's title, body, tags, or project.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"note_id": map[string]interface{}{
					"type":        "string",
					"description": "The note ID to update",
				},
				"title": map[string]interface{}{
					"type":        "string",
					"description": "New title (optional)",
				},
				"body": map[string]interface{}{
					"type":        "string",
					"description": "New body (optional)",
				},
				"tags": map[string]interface{}{
					"type":        "array",
					"items":       map[string]interface{}{"type": "string"},
					"description": "New tags (optional)",
				},
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "New project ID (optional)",
				},
			},
			"required": []string{"note_id"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				NoteID    string   `json:"note_id"`
				Title     *string  `json:"title"`
				Body      *string  `json:"body"`
				Tags      []string `json:"tags"`
				ProjectID *string  `json:"project_id"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			req := note.UpdateNoteReq{
				Title:     params.Title,
				Body:      params.Body,
				ProjectID: params.ProjectID,
			}
			if params.Tags != nil {
				req.Tags = &params.Tags
			}
			n, err := noteSvc.Update(ctx, userID, params.NoteID, req)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":    n.ID,
				"title": n.Title,
			})
		},
		ReadOnly: false,
	})
}

func registerAppendToNote(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "append_to_note",
		Description: "Append text content to an existing note.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"note_id": map[string]interface{}{
					"type":        "string",
					"description": "The note ID to append to",
				},
				"text": map[string]interface{}{
					"type":        "string",
					"description": "The text to append",
				},
			},
			"required": []string{"note_id", "text"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				NoteID string `json:"note_id"`
				Text   string `json:"text"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			n, err := noteSvc.AppendToNote(ctx, userID, params.NoteID, params.Text)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":    n.ID,
				"title": n.Title,
			})
		},
		ReadOnly: false,
	})
}

func registerListNotes(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "list_notes",
		Description: "List notes with optional filters for project, tag, and sort order.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by project ID",
				},
				"tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag name",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default 20, max 50)",
				},
				"sort": map[string]interface{}{
					"type":        "string",
					"description": "Sort field: updated_at (default), created_at, title",
				},
			},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				ProjectID string `json:"project_id"`
				Tag       string `json:"tag"`
				Limit     int    `json:"limit"`
				Sort      string `json:"sort"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 || params.Limit > 50 {
				params.Limit = 20
			}
			filter := note.NoteFilter{
				ProjectID:   params.ProjectID,
				Tag:         params.Tag,
				Limit:       params.Limit,
				Sort:        params.Sort,
				ExcludeBody: true,
			}
			notes, total, err := noteSvc.List(ctx, userID, filter)
			if err != nil {
				return nil, err
			}
			type slimNote struct {
				ID        string   `json:"id"`
				Title     string   `json:"title"`
				Tags      []string `json:"tags"`
				ProjectID string   `json:"project_id,omitempty"`
				UpdatedAt string   `json:"updated_at"`
			}
			slim := make([]slimNote, 0, len(notes))
			for _, n := range notes {
				slim = append(slim, slimNote{
					ID:        n.ID,
					Title:     n.Title,
					Tags:      n.Tags,
					ProjectID: n.ProjectID,
					UpdatedAt: n.UpdatedAt.Format(time.RFC3339),
				})
			}
			return marshalResult(map[string]interface{}{
				"notes": slim,
				"total": total,
			})
		},
		ReadOnly: true,
	})
}

func registerListProjects(registry *ToolRegistry, projectSvc *project.Service) {
	registry.Register(&Tool{
		Name:        "list_projects",
		Description: "List all projects.",
		Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			projects, err := projectSvc.List(ctx, userID)
			if err != nil {
				return nil, err
			}
			type slimProject struct {
				ID          string `json:"id"`
				Name        string `json:"name"`
				Slug        string `json:"slug"`
				Description string `json:"description"`
			}
			slim := make([]slimProject, 0, len(projects))
			for _, p := range projects {
				slim = append(slim, slimProject{
					ID:          p.ID,
					Name:        p.Name,
					Slug:        p.Slug,
					Description: p.Description,
				})
			}
			return marshalResult(map[string]interface{}{
				"projects": slim,
			})
		},
		ReadOnly: true,
	})
}

func registerCreateProject(registry *ToolRegistry, projectSvc *project.Service) {
	registry.Register(&Tool{
		Name:        "create_project",
		Description: "Create a new project.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"name": map[string]interface{}{
					"type":        "string",
					"description": "The project name",
				},
				"description": map[string]interface{}{
					"type":        "string",
					"description": "Optional description",
				},
			},
			"required": []string{"name"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Name        string `json:"name"`
				Description string `json:"description"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			p, err := projectSvc.Create(ctx, userID, params.Name, params.Description)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":   p.ID,
				"name": p.Name,
				"slug": p.Slug,
			})
		},
		ReadOnly: false,
	})
}

func registerListTasks(registry *ToolRegistry, taskSvc *task.Service) {
	registry.Register(&Tool{
		Name:        "list_tasks",
		Description: "List tasks with optional filters for project, tag, and done status.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_slug": map[string]interface{}{
					"type":        "string",
					"description": "Filter by project slug",
				},
				"done": map[string]interface{}{
					"type":        "boolean",
					"description": "Filter by done status",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default 20, max 100)",
				},
			},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				ProjectSlug string `json:"project_slug"`
				Done        *bool  `json:"done"`
				Limit       int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 || params.Limit > 100 {
				params.Limit = 20
			}
			filter := task.TaskFilter{
				ProjectSlug: params.ProjectSlug,
				Done:        params.Done,
				Limit:       params.Limit,
			}
			tasks, total, err := taskSvc.List(ctx, userID, filter)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"tasks": tasks,
				"total": total,
			})
		},
		ReadOnly: true,
	})
}

func registerToggleTask(registry *ToolRegistry, taskSvc *task.Service) {
	registry.Register(&Tool{
		Name:        "toggle_task",
		Description: "Mark a task as done or undone.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"task_id": map[string]interface{}{
					"type":        "string",
					"description": "The task ID",
				},
				"done": map[string]interface{}{
					"type":        "boolean",
					"description": "True to mark done, false to mark undone",
				},
			},
			"required": []string{"task_id", "done"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				TaskID string `json:"task_id"`
				Done   bool   `json:"done"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if err := taskSvc.ToggleDone(ctx, userID, params.TaskID, params.Done); err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"task_id": params.TaskID,
				"done":    params.Done,
			})
		},
		ReadOnly: false,
	})
}

func registerGetDailyNote(registry *ToolRegistry, noteSvc *note.Service) {
	registry.Register(&Tool{
		Name:        "get_daily_note",
		Description: "Get or create today's daily note.",
		Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			n, err := noteSvc.GetOrCreateDaily(ctx, userID, time.Now())
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":    n.ID,
				"title": n.Title,
				"body":  n.Body,
			})
		},
		ReadOnly: true,
	})
}

func registerGetGraph(registry *ToolRegistry, graphSvc *graph.Service) {
	registry.Register(&Tool{
		Name:        "get_graph",
		Description: "Query the knowledge graph. Returns nodes and edges.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"project_id": map[string]interface{}{
					"type":        "string",
					"description": "Filter by project ID",
				},
				"tag": map[string]interface{}{
					"type":        "string",
					"description": "Filter by tag",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum nodes (default 100, max 500)",
				},
			},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				ProjectID string `json:"project_id"`
				Tag       string `json:"tag"`
				Limit     int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 || params.Limit > 500 {
				params.Limit = 100
			}
			filter := graph.GraphFilter{
				ProjectID: params.ProjectID,
				Tag:       params.Tag,
				Limit:     params.Limit,
			}
			g, err := graphSvc.GetGraph(ctx, userID, filter)
			if err != nil {
				return nil, err
			}
			return marshalResult(g)
		},
		ReadOnly: true,
	})
}

func registerFindRelated(registry *ToolRegistry, searchSvc *search.Service) {
	registry.Register(&Tool{
		Name:        "find_related",
		Description: "Find notes semantically related to a search query.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The semantic search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default 10, max 30)",
				},
			},
			"required": []string{"query"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 || params.Limit > 30 {
				params.Limit = 10
			}
			results, err := searchSvc.SearchSemantic(ctx, userID, params.Query, params.Limit)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"results": results,
			})
		},
		ReadOnly: true,
	})
}

func registerGetCurrentTime(registry *ToolRegistry) {
	registry.Register(&Tool{
		Name:        "get_current_time",
		Description: "Get the current date and time.",
		Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			now := time.Now()
			return marshalResult(map[string]interface{}{
				"datetime":    now.Format(time.RFC3339),
				"date":        now.Format("2006-01-02"),
				"time":        now.Format("15:04:05"),
				"day_of_week": now.Weekday().String(),
				"timezone":    now.Location().String(),
			})
		},
		ReadOnly: true,
	})
}

func registerSearchConversations(registry *ToolRegistry, chatSvc *chat.Service) {
	registry.Register(&Tool{
		Name:        "search_conversations",
		Description: "Search across all past conversations for messages containing the query. Useful for recalling previous discussions.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default 10)",
				},
			},
			"required": []string{"query"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 {
				params.Limit = 10
			}
			messages, err := chatSvc.SearchMessages(ctx, userID, params.Query, params.Limit)
			if err != nil {
				return nil, err
			}
			type slimMessage struct {
				ConversationID string `json:"conversation_id"`
				Role           string `json:"role"`
				Content        string `json:"content"`
				CreatedAt      string `json:"created_at"`
			}
			slim := make([]slimMessage, 0, len(messages))
			for _, m := range messages {
				content := m.Content
				runes := []rune(content)
				if len(runes) > 500 {
					content = string(runes[:500]) + "..."
				}
				slim = append(slim, slimMessage{
					ConversationID: m.ConversationID,
					Role:           m.Role,
					Content:        content,
					CreatedAt:      m.CreatedAt,
				})
			}
			return marshalResult(map[string]interface{}{
				"messages": slim,
				"count":    len(slim),
			})
		},
		ReadOnly: true,
	})
}

func registerSaveMemory(registry *ToolRegistry, svc *Service) {
	registry.Register(&Tool{
		Name:        "save_memory",
		Description: "Save a fact, preference, decision, or commitment to long-term memory. Use this when the user shares important information about themselves, their preferences, or decisions.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"content": map[string]interface{}{
					"type":        "string",
					"description": "The memory content to save",
				},
				"category": map[string]interface{}{
					"type":        "string",
					"description": "Category: fact, preference, decision, or commitment",
					"enum":        []string{"fact", "preference", "decision", "commitment"},
				},
			},
			"required": []string{"content", "category"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Content  string `json:"content"`
				Category string `json:"category"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			m := &Memory{
				Content:  params.Content,
				Category: params.Category,
				Source:   "assistant",
			}
			if err := svc.CreateMemory(ctx, userID, m); err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"id":       m.ID,
				"category": m.Category,
				"saved":    true,
			})
		},
		ReadOnly: false,
	})
}

func registerSearchMemories(registry *ToolRegistry, svc *Service) {
	registry.Register(&Tool{
		Name:        "search_memories",
		Description: "Search long-term memories for previously saved facts, preferences, and decisions.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"query": map[string]interface{}{
					"type":        "string",
					"description": "The search query",
				},
				"limit": map[string]interface{}{
					"type":        "integer",
					"description": "Maximum results (default 10)",
				},
			},
			"required": []string{"query"},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params struct {
				Query string `json:"query"`
				Limit int    `json:"limit"`
			}
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if params.Limit <= 0 {
				params.Limit = 10
			}
			memories, err := svc.SearchMemories(ctx, userID, params.Query, params.Limit)
			if err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"memories": memories,
				"count":    len(memories),
			})
		},
		ReadOnly: true,
	})
}

func registerGetProfile(registry *ToolRegistry, svc *Service) {
	registry.Register(&Tool{
		Name:        "get_profile",
		Description: "Get the user's profile information (name, profession, goals, preferences, etc.).",
		Parameters:  mustJSON(map[string]interface{}{"type": "object", "properties": map[string]interface{}{}}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			profile, err := svc.GetProfile(ctx, userID)
			if err != nil {
				return nil, err
			}
			return marshalResult(profile)
		},
		ReadOnly: true,
	})
}

func registerUpdateProfile(registry *ToolRegistry, svc *Service) {
	registry.Register(&Tool{
		Name:        "update_profile",
		Description: "Update the user's profile. Use this when the user shares personal information like their name, profession, goals, or communication preferences.",
		Parameters: mustJSON(map[string]interface{}{
			"type": "object",
			"properties": map[string]interface{}{
				"display_name": map[string]interface{}{
					"type":        "string",
					"description": "User's display name",
				},
				"profession": map[string]interface{}{
					"type":        "string",
					"description": "User's profession or role",
				},
				"organization": map[string]interface{}{
					"type":        "string",
					"description": "User's organization",
				},
				"goals": map[string]interface{}{
					"type":        "string",
					"description": "User's current goals or focus areas",
				},
				"interests": map[string]interface{}{
					"type":        "string",
					"description": "User's topics of interest",
				},
				"timezone": map[string]interface{}{
					"type":        "string",
					"description": "User's timezone (e.g., America/New_York)",
				},
				"communication": map[string]interface{}{
					"type":        "string",
					"description": "Preferred communication style: concise, detailed, formal, casual",
				},
				"instructions": map[string]interface{}{
					"type":        "string",
					"description": "Custom instructions for the assistant",
				},
			},
		}),
		Func: func(ctx context.Context, userID string, args json.RawMessage) (json.RawMessage, error) {
			var params UserProfile
			if err := json.Unmarshal(args, &params); err != nil {
				return nil, fmt.Errorf("%w: %w", ErrInvalidArguments, err)
			}
			if err := svc.UpdateProfile(ctx, userID, &params); err != nil {
				return nil, err
			}
			return marshalResult(map[string]interface{}{
				"updated": true,
			})
		},
		ReadOnly: false,
	})
}

// mustJSON marshals a value to JSON or panics. Used for static schema definitions.
func mustJSON(v interface{}) json.RawMessage {
	b, err := json.Marshal(v)
	if err != nil {
		panic(fmt.Sprintf("assistant.mustJSON: %v", err))
	}
	return b
}

// marshalResult marshals a tool result to json.RawMessage. Errors are
// propagated up so the agentic loop can surface them via the standard
// tr.Error path -- never inject a fake JSON object that the LLM might
// mistake for a real result.
func marshalResult(v interface{}) (json.RawMessage, error) {
	b, err := json.Marshal(v)
	if err != nil {
		return nil, fmt.Errorf("assistant.marshalResult: %w", err)
	}
	return b, nil
}
