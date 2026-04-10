package main

import (
	"fmt"
	"strings"
	"time"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

const searchDebounce = 300 * time.Millisecond

// searchMode distinguishes full-text from semantic search.
type searchMode int

const (
	searchModeFTS searchMode = iota
	searchModeSemantic
)

// unifiedResult is a search result from either FTS or semantic search.
type unifiedResult struct {
	NoteID  string
	Title   string
	Snippet string
	Score   float64 // only set for semantic results
}

// searchModel handles the search screen.
type searchModel struct {
	client    *APIClient
	input     textinput.Model
	results   []unifiedResult
	resultIdx int
	mode      searchMode
	err       string
	loading   bool
	done      bool
	width     int
	height    int
	lastQuery string
	timerID   int // monotonic counter to identify debounce ticks
}

// openSearchMsg triggers switching to the search screen.
type openSearchMsg struct{}

// searchResultsMsg delivers search results.
type searchResultsMsg struct {
	results []unifiedResult
	query   string
}

// searchTickMsg fires after the debounce interval.
type searchTickMsg struct {
	query   string
	timerID int
}

func newSearchModel(client *APIClient, width, height int) searchModel {
	ti := textinput.New()
	ti.Placeholder = "Search notes..."
	ti.CharLimit = 256
	ti.SetWidth(60)
	ti.Focus()

	return searchModel{
		client: client,
		input:  ti,
		width:  width,
		height: height,
	}
}

func (m searchModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m searchModel) Update(msg tea.Msg) (searchModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case searchResultsMsg:
		m.loading = false
		// Only accept results for the current query.
		if msg.query == m.lastQuery {
			m.results = msg.results
			m.resultIdx = 0
		}
		return m, nil

	case apiErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case searchTickMsg:
		// Only fire the search if no newer tick has been scheduled.
		if msg.timerID != m.timerID {
			return m, nil
		}
		query := msg.query
		if query == "" {
			return m, nil
		}
		m.loading = true
		client := m.client
		mode := m.mode
		return m, func() tea.Msg {
			if mode == searchModeSemantic {
				results, err := client.SearchSemantic(query)
				if err != nil {
					return apiErrorMsg{err: err}
				}
				unified := make([]unifiedResult, len(results))
				for i, r := range results {
					unified[i] = unifiedResult{
						NoteID:  r.NoteID,
						Title:   r.Title,
						Snippet: r.Snippet,
						Score:   r.Score,
					}
				}
				return searchResultsMsg{results: unified, query: query}
			}
			results, err := client.Search(query)
			if err != nil {
				return apiErrorMsg{err: err}
			}
			unified := make([]unifiedResult, len(results))
			for i, r := range results {
				unified[i] = unifiedResult{
					NoteID:  r.NoteID,
					Title:   r.Title,
					Snippet: r.Snippet,
				}
			}
			return searchResultsMsg{results: unified, query: query}
		}

	case tea.KeyPressMsg:
		m.err = ""
		switch msg.String() {
		case "esc":
			m.done = true
			return m, nil

		case "down", "ctrl+n":
			if m.resultIdx < len(m.results)-1 {
				m.resultIdx++
			}
			return m, nil

		case "up", "ctrl+p":
			if m.resultIdx > 0 {
				m.resultIdx--
			}
			return m, nil

		case "enter":
			if len(m.results) > 0 {
				noteID := m.results[m.resultIdx].NoteID
				return m, func() tea.Msg {
					return openEditorMsg{noteID: noteID}
				}
			}
			return m, nil
		}
	}

	// Update the text input and schedule a debounced search.
	var cmd tea.Cmd
	m.input, cmd = m.input.Update(msg)

	rawQuery := strings.TrimSpace(m.input.Value())

	// Detect semantic search prefix "?".
	if strings.HasPrefix(rawQuery, "?") {
		m.mode = searchModeSemantic
		rawQuery = strings.TrimSpace(rawQuery[1:])
	} else {
		m.mode = searchModeFTS
	}

	if rawQuery != m.lastQuery {
		m.lastQuery = rawQuery
		// Only trigger search for queries with 2+ characters.
		if len(rawQuery) < 2 {
			m.results = nil
			m.resultIdx = 0
			return m, cmd
		}
		m.timerID++
		currentID := m.timerID
		currentQuery := rawQuery
		debounceCmd := tea.Tick(searchDebounce, func(time.Time) tea.Msg {
			return searchTickMsg{query: currentQuery, timerID: currentID}
		})
		return m, tea.Batch(cmd, debounceCmd)
	}

	return m, cmd
}

func (m searchModel) View() string {
	if m.width == 0 {
		return ""
	}

	// Header.
	modeLabel := "Full-text"
	if m.mode == searchModeSemantic {
		modeLabel = "Semantic"
	}
	header := styles.Header.Width(m.width).Render(fmt.Sprintf(" Search (%s)", modeLabel))

	// Search input.
	var b strings.Builder
	b.WriteString("\n ")
	b.WriteString(m.input.View())
	b.WriteString("\n")

	if m.mode == searchModeSemantic {
		b.WriteString(styles.Muted.Render("  Prefix ? for semantic search"))
		b.WriteString("\n")
	}

	if m.loading {
		b.WriteString(styles.Muted.Render("  Searching..."))
		b.WriteString("\n")
	}

	if m.err != "" {
		b.WriteString("  ")
		b.WriteString(styles.Error.Render(m.err))
		b.WriteString("\n")
	}

	b.WriteString("\n")

	if m.lastQuery != "" && len(m.lastQuery) < 2 {
		b.WriteString(styles.Muted.Render("  Type 2+ characters to search"))
		b.WriteString("\n")
	} else if len(m.results) == 0 && m.lastQuery != "" && !m.loading {
		b.WriteString(styles.Muted.Render("  No results found"))
		b.WriteString("\n")
	}

	maxResults := m.height - 8
	if maxResults < 3 {
		maxResults = 3
	}

	for i, r := range m.results {
		if i >= maxResults {
			b.WriteString(styles.Muted.Render(fmt.Sprintf("  ... +%d more results", len(m.results)-i)))
			b.WriteString("\n")
			break
		}

		titleStr := r.Title
		if titleStr == "" {
			titleStr = "(untitled)"
		}

		// Append similarity score for semantic results.
		displayTitle := titleStr
		if m.mode == searchModeSemantic && r.Score > 0 {
			displayTitle = fmt.Sprintf("%s  [%d%%]", titleStr, int(r.Score*100))
		}

		if i == m.resultIdx {
			b.WriteString(styles.Selected.Width(m.width - 4).Render("> " + displayTitle))
		} else {
			b.WriteString(styles.Normal.Width(m.width - 4).Render("  " + displayTitle))
		}
		b.WriteString("\n")

		// Show snippet below the title.
		if r.Snippet != "" {
			// Strip HTML tags from snippet for terminal display.
			snippet := stripHTML(r.Snippet)
			if m.width > 14 {
				runes := []rune(snippet)
				if len(runes) > m.width-8 {
					snippet = string(runes[:m.width-11]) + "..."
				}
			}
			b.WriteString(styles.Muted.Render("    " + snippet))
			b.WriteString("\n")
		}
	}

	// Status bar.
	statusBar := styles.StatusBar.Width(m.width).Render("Enter: open | Up/Down: navigate | ?query: semantic | Esc: back")

	content := b.String()
	return lipgloss.JoinVertical(lipgloss.Left, header, content, statusBar)
}

// stripHTML removes HTML tags from a string for terminal display.
func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	for _, r := range s {
		if r == '<' {
			inTag = true
			continue
		}
		if r == '>' {
			inTag = false
			continue
		}
		if !inTag {
			b.WriteRune(r)
		}
	}
	return b.String()
}
