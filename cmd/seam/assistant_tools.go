package main

import (
	"encoding/json"
	"fmt"
	"strings"
)

// renderToolResult dispatches to a per-tool formatter. Returns a
// human-readable, styled string. Unknown tools fall back to a
// pretty-printed JSON dump truncated to 10 lines.
func renderToolResult(toolName string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return assistantStyles.Muted.Render("(empty result)")
	}

	switch toolName {
	case "search_notes", "find_related", "list_notes":
		return renderNoteList(raw)
	case "read_note", "get_daily_note":
		return renderNoteContent(raw)
	case "create_note":
		return renderNoteOp("Created note", raw)
	case "update_note":
		return renderNoteOp("Updated note", raw)
	case "append_to_note":
		return renderNoteOp("Appended to note", raw)
	case "create_project":
		return renderProjectCreate(raw)
	case "list_projects":
		return renderProjectChips(raw)
	case "list_tasks":
		return renderTaskList(raw)
	case "toggle_task":
		return renderTaskToggle(raw)
	case "get_current_time":
		return renderCurrentTime(raw)
	case "get_graph":
		return renderGraphSummary(raw)
	case "search_conversations":
		return renderConversationSearch(raw)
	case "save_memory":
		return renderSaveMemory(raw)
	case "search_memories":
		return renderMemoryList(raw)
	case "get_profile":
		return renderProfile(raw)
	case "update_profile":
		return assistantStyles.ToolStatusOk.Render("✔ ") + assistantStyles.ToolBlock.Render(marioBlock+" Profile updated")
	default:
		return fallback(raw)
	}
}

// fallback renders any JSON result as a pretty-printed, truncated block.
func fallback(raw json.RawMessage) string {
	var v interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		// Not valid JSON -- show raw bytes.
		return assistantStyles.Muted.Render(strings.TrimSpace(string(raw)))
	}
	pretty, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return assistantStyles.Muted.Render(strings.TrimSpace(string(raw)))
	}
	lines := strings.Split(string(pretty), "\n")
	const maxLines = 10
	truncated := false
	remaining := 0
	if len(lines) > maxLines {
		remaining = len(lines) - maxLines
		lines = lines[:maxLines]
		truncated = true
	}
	out := assistantStyles.Muted.Render(strings.Join(lines, "\n"))
	if truncated {
		out += "\n" + assistantStyles.Muted.Render(fmt.Sprintf("... (%d more lines)", remaining))
	}
	return out
}

// -- Per-tool formatters -----------------------------------------------------

type toolNote struct {
	ID      string   `json:"id"`
	NoteID  string   `json:"note_id"`
	Title   string   `json:"title"`
	Project string   `json:"project"`
	Tags    []string `json:"tags"`
	Snippet string   `json:"snippet"`
	Body    string   `json:"body"`
}

// noteListPayload accepts both a bare array and an object wrapper.
func decodeNoteList(raw json.RawMessage) ([]toolNote, bool) {
	var direct []toolNote
	if err := json.Unmarshal(raw, &direct); err == nil {
		return direct, true
	}
	var wrapper struct {
		Notes   []toolNote `json:"notes"`
		Results []toolNote `json:"results"`
	}
	if err := json.Unmarshal(raw, &wrapper); err == nil {
		if len(wrapper.Notes) > 0 {
			return wrapper.Notes, true
		}
		if len(wrapper.Results) > 0 {
			return wrapper.Results, true
		}
	}
	return nil, false
}

func renderNoteList(raw json.RawMessage) string {
	notes, ok := decodeNoteList(raw)
	if !ok {
		return fallback(raw)
	}
	if len(notes) == 0 {
		return assistantStyles.Muted.Render("(no notes)")
	}
	var out []string
	for i, n := range notes {
		title := n.Title
		if title == "" {
			title = "(untitled)"
		}
		meta := ""
		if n.Project != "" || len(n.Tags) > 0 {
			parts := []string{}
			if n.Project != "" {
				parts = append(parts, n.Project)
			}
			if len(n.Tags) > 0 {
				parts = append(parts, strings.Join(n.Tags, ", "))
			}
			meta = " " + assistantStyles.Muted.Render("("+strings.Join(parts, " · ")+")")
		}
		head := fmt.Sprintf("[%d] %s%s", i+1, assistantStyles.MessageAssist.Render(title), meta)
		out = append(out, head)
		snippet := strings.TrimSpace(n.Snippet)
		if snippet != "" {
			out = append(out, "    "+assistantStyles.Muted.Render(truncateOneLine(snippet, 120)))
		}
	}
	return strings.Join(out, "\n")
}

func renderNoteContent(raw json.RawMessage) string {
	var n toolNote
	if err := json.Unmarshal(raw, &n); err != nil {
		return fallback(raw)
	}
	title := n.Title
	if title == "" {
		title = "(untitled)"
	}
	var out []string
	out = append(out, assistantStyles.ToolBlock.Render(title))
	if len(n.Tags) > 0 {
		out = append(out, assistantStyles.Muted.Render("  tags: ")+assistantStyles.MessageUser.Render(strings.Join(n.Tags, ", ")))
	}
	body := strings.TrimSpace(n.Body)
	if body == "" {
		body = strings.TrimSpace(n.Snippet)
	}
	if body != "" {
		lines := strings.Split(body, "\n")
		const maxLines = 4
		if len(lines) > maxLines {
			lines = lines[:maxLines]
		}
		for _, l := range lines {
			out = append(out, assistantStyles.MessageAssist.Render("  "+l))
		}
	}
	return strings.Join(out, "\n")
}

func renderNoteOp(action string, raw json.RawMessage) string {
	var n toolNote
	if err := json.Unmarshal(raw, &n); err != nil {
		return fallback(raw)
	}
	title := n.Title
	if title == "" {
		title = "(untitled)"
	}
	return assistantStyles.ToolStatusOk.Render("✔ ") +
		assistantStyles.ToolBlock.Render(marioBlock+" "+action+": ") +
		assistantStyles.MessageAssist.Render(title)
}

func renderProjectCreate(raw json.RawMessage) string {
	var p struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &p); err != nil {
		return fallback(raw)
	}
	name := p.Name
	if name == "" {
		name = "(unnamed)"
	}
	suffix := ""
	if p.Slug != "" {
		suffix = " " + assistantStyles.Muted.Render("("+p.Slug+")")
	}
	return assistantStyles.ToolStatusOk.Render("✔ ") +
		assistantStyles.ToolBlock.Render(marioBlock+" Created project: ") +
		assistantStyles.MessageAssist.Render(name) + suffix
}

func renderProjectChips(raw json.RawMessage) string {
	var direct []struct {
		Name string `json:"name"`
		Slug string `json:"slug"`
	}
	if err := json.Unmarshal(raw, &direct); err != nil {
		var wrapper struct {
			Projects []struct {
				Name string `json:"name"`
				Slug string `json:"slug"`
			} `json:"projects"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return fallback(raw)
		}
		direct = wrapper.Projects
	}
	if len(direct) == 0 {
		return assistantStyles.Muted.Render("(no projects)")
	}
	names := make([]string, 0, len(direct))
	for _, p := range direct {
		n := p.Name
		if n == "" {
			n = p.Slug
		}
		names = append(names, assistantStyles.MessageAssist.Render(n))
	}
	return strings.Join(names, assistantStyles.Muted.Render(", "))
}

func renderTaskList(raw json.RawMessage) string {
	var direct []struct {
		Content string `json:"content"`
		Done    bool   `json:"done"`
	}
	if err := json.Unmarshal(raw, &direct); err != nil {
		var wrapper struct {
			Tasks []struct {
				Content string `json:"content"`
				Done    bool   `json:"done"`
			} `json:"tasks"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return fallback(raw)
		}
		direct = wrapper.Tasks
	}
	if len(direct) == 0 {
		return assistantStyles.Muted.Render("(no tasks)")
	}
	var out []string
	for _, t := range direct {
		box := "[ ]"
		if t.Done {
			box = "[x]"
		}
		style := assistantStyles.MessageAssist
		if t.Done {
			style = assistantStyles.Muted
		}
		out = append(out, assistantStyles.ToolBlock.Render(box)+" "+style.Render(strings.TrimSpace(t.Content)))
	}
	return strings.Join(out, "\n")
}

func renderTaskToggle(raw json.RawMessage) string {
	var t struct {
		Done    bool   `json:"done"`
		Content string `json:"content"`
	}
	if err := json.Unmarshal(raw, &t); err != nil {
		return fallback(raw)
	}
	label := "Marked task pending"
	if t.Done {
		label = "Marked task done"
	}
	return assistantStyles.ToolStatusOk.Render("✔ ") + assistantStyles.ToolBlock.Render(marioBlock+" "+label)
}

func renderCurrentTime(raw json.RawMessage) string {
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil && asString != "" {
		return assistantStyles.MessageAssist.Render(asString)
	}
	var obj struct {
		Time string `json:"time"`
		Now  string `json:"now"`
		Date string `json:"date"`
	}
	if err := json.Unmarshal(raw, &obj); err == nil {
		v := obj.Time
		if v == "" {
			v = obj.Now
		}
		if v == "" {
			v = obj.Date
		}
		if v != "" {
			return assistantStyles.MessageAssist.Render(v)
		}
	}
	return fallback(raw)
}

func renderGraphSummary(raw json.RawMessage) string {
	var g struct {
		Nodes []json.RawMessage `json:"nodes"`
		Edges []json.RawMessage `json:"edges"`
	}
	if err := json.Unmarshal(raw, &g); err != nil {
		return fallback(raw)
	}
	return assistantStyles.ToolBlock.Render(marioBlock+" ") +
		assistantStyles.MessageAssist.Render(fmt.Sprintf("%d nodes, %d edges", len(g.Nodes), len(g.Edges))) +
		" " + assistantStyles.Muted.Render("(open the web for visualization)")
}

func renderConversationSearch(raw json.RawMessage) string {
	var direct []struct {
		Role    string `json:"role"`
		Content string `json:"content"`
		Snippet string `json:"snippet"`
	}
	if err := json.Unmarshal(raw, &direct); err != nil {
		var wrapper struct {
			Results []struct {
				Role    string `json:"role"`
				Content string `json:"content"`
				Snippet string `json:"snippet"`
			} `json:"results"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return fallback(raw)
		}
		direct = wrapper.Results
	}
	if len(direct) == 0 {
		return assistantStyles.Muted.Render("(no matches)")
	}
	var out []string
	for _, r := range direct {
		body := r.Snippet
		if body == "" {
			body = r.Content
		}
		body = truncateOneLine(strings.TrimSpace(body), 120)
		role := r.Role
		if role == "" {
			role = "?"
		}
		out = append(out, assistantStyles.ToolBlock.Render(role+":")+" "+assistantStyles.MessageAssist.Render(body))
	}
	return strings.Join(out, "\n")
}

func renderSaveMemory(raw json.RawMessage) string {
	var m struct {
		Category string `json:"category"`
	}
	if err := json.Unmarshal(raw, &m); err != nil {
		return fallback(raw)
	}
	cat := m.Category
	if cat == "" {
		cat = "general"
	}
	return assistantStyles.ToolStatusOk.Render("✔ ") +
		assistantStyles.ToolBlock.Render(marioBlock+" Saved memory in ") +
		assistantStyles.MessageUser.Render(cat)
}

func renderMemoryList(raw json.RawMessage) string {
	var direct []struct {
		Content  string `json:"content"`
		Category string `json:"category"`
	}
	if err := json.Unmarshal(raw, &direct); err != nil {
		var wrapper struct {
			Memories []struct {
				Content  string `json:"content"`
				Category string `json:"category"`
			} `json:"memories"`
		}
		if err := json.Unmarshal(raw, &wrapper); err != nil {
			return fallback(raw)
		}
		direct = wrapper.Memories
	}
	if len(direct) == 0 {
		return assistantStyles.Muted.Render("(no memories)")
	}
	var out []string
	for i, m := range direct {
		cat := m.Category
		if cat == "" {
			cat = "general"
		}
		body := truncateOneLine(strings.TrimSpace(m.Content), 120)
		head := fmt.Sprintf("[%d] ", i+1) + assistantStyles.MessageUser.Render(cat)
		out = append(out, head)
		out = append(out, "    "+assistantStyles.MessageAssist.Render(body))
	}
	return strings.Join(out, "\n")
}

func renderProfile(raw json.RawMessage) string {
	var v map[string]interface{}
	if err := json.Unmarshal(raw, &v); err != nil {
		return fallback(raw)
	}
	if len(v) == 0 {
		return assistantStyles.Muted.Render("(empty profile)")
	}
	var out []string
	for k, val := range v {
		out = append(out, assistantStyles.ToolBlock.Render(k+":")+" "+assistantStyles.MessageAssist.Render(stringifyValue(val)))
	}
	return strings.Join(out, "\n")
}

// -- Small helpers -----------------------------------------------------------

func truncateOneLine(s string, max int) string {
	s = strings.ReplaceAll(s, "\n", " ")
	runes := []rune(s)
	if len(runes) <= max {
		return s
	}
	if max < 4 {
		return string(runes[:max])
	}
	return string(runes[:max-3]) + "..."
}

func stringifyValue(v interface{}) string {
	switch t := v.(type) {
	case nil:
		return ""
	case string:
		return t
	case bool:
		if t {
			return "true"
		}
		return "false"
	case float64:
		// JSON decodes all numbers as float64. Print integers without a
		// trailing ".0" where possible.
		if t == float64(int64(t)) {
			return fmt.Sprintf("%d", int64(t))
		}
		return fmt.Sprintf("%g", t)
	case []interface{}:
		parts := make([]string, 0, len(t))
		for _, item := range t {
			parts = append(parts, stringifyValue(item))
		}
		return strings.Join(parts, ", ")
	default:
		b, err := json.Marshal(t)
		if err != nil {
			return fmt.Sprintf("%v", t)
		}
		return string(b)
	}
}
