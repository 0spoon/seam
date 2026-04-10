package main

import (
	"fmt"
	"sort"
	"strings"
	"time"

	tea "charm.land/bubbletea/v2"
	"charm.land/lipgloss/v2"
)

// openTimelineMsg signals to switch to the timeline view.
type openTimelineMsg struct{}

// timelineGroup holds notes grouped by date.
type timelineGroup struct {
	date    string // YYYY-MM-DD
	display string // e.g., "Mar 8, 2026"
	isToday bool
	notes   []*Note
}

type timelineModel struct {
	client   *APIClient
	width    int
	height   int
	groups   []timelineGroup
	groupIdx int
	noteIdx  int
	loading  bool
	err      string
	sortMode string // "created" or "modified"
	done     bool
}

func newTimelineModel(client *APIClient, w, h int) timelineModel {
	return timelineModel{
		client:   client,
		width:    w,
		height:   h,
		sortMode: "modified",
		loading:  true,
	}
}

type timelineLoadedMsg struct {
	notes []*Note
}

type timelineErrorMsg struct {
	err error
}

func (m timelineModel) Init() tea.Cmd {
	return m.loadNotes()
}

func (m timelineModel) loadNotes() tea.Cmd {
	client := m.client
	sortMode := m.sortMode
	return func() tea.Msg {
		notes, err := client.ListNotesAll(sortMode, 500)
		if err != nil {
			return timelineErrorMsg{err: err}
		}
		return timelineLoadedMsg{notes: notes}
	}
}

func groupByDate(notes []*Note, mode string) []timelineGroup {
	groups := make(map[string][]*Note)
	for _, n := range notes {
		dateStr := n.UpdatedAt
		if mode == "created" {
			dateStr = n.CreatedAt
		}
		t, err := time.Parse(time.RFC3339, dateStr)
		if err != nil {
			continue
		}
		key := t.Format("2006-01-02")
		groups[key] = append(groups[key], n)
	}

	// Sort keys descending.
	keys := make([]string, 0, len(groups))
	for k := range groups {
		keys = append(keys, k)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(keys)))

	today := time.Now().Format("2006-01-02")
	result := make([]timelineGroup, 0, len(keys))
	for _, k := range keys {
		t, _ := time.Parse("2006-01-02", k)
		result = append(result, timelineGroup{
			date:    k,
			display: t.Format("Jan 2, 2006"),
			isToday: k == today,
			notes:   groups[k],
		})
	}
	return result
}

func (m timelineModel) Update(msg tea.Msg) (timelineModel, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		return m, nil

	case timelineLoadedMsg:
		m.loading = false
		m.groups = groupByDate(msg.notes, m.sortMode)
		m.groupIdx = 0
		m.noteIdx = 0
		return m, nil

	case timelineErrorMsg:
		m.loading = false
		m.err = msg.err.Error()
		return m, nil

	case tea.KeyPressMsg:
		m.err = ""
		switch msg.String() {
		case "q", "esc":
			m.done = true
			return m, nil

		case "]", "l":
			// Next date group.
			if m.groupIdx < len(m.groups)-1 {
				m.groupIdx++
				m.noteIdx = 0
			}
			return m, nil

		case "[", "h":
			// Previous date group.
			if m.groupIdx > 0 {
				m.groupIdx--
				m.noteIdx = 0
			}
			return m, nil

		case "j", "down":
			if len(m.groups) > 0 {
				g := m.groups[m.groupIdx]
				if m.noteIdx < len(g.notes)-1 {
					m.noteIdx++
				}
			}
			return m, nil

		case "k", "up":
			if m.noteIdx > 0 {
				m.noteIdx--
			}
			return m, nil

		case "enter":
			if len(m.groups) > 0 && m.groupIdx < len(m.groups) {
				g := m.groups[m.groupIdx]
				if m.noteIdx < len(g.notes) {
					note := g.notes[m.noteIdx]
					return m, func() tea.Msg {
						return openEditorMsg{noteID: note.ID}
					}
				}
			}
			return m, nil

		case "s":
			// Toggle sort mode.
			if m.sortMode == "modified" {
				m.sortMode = "created"
			} else {
				m.sortMode = "modified"
			}
			m.loading = true
			return m, m.loadNotes()
		}
	}

	return m, nil
}

func (m timelineModel) View() string {
	if m.loading {
		return styles.Muted.Render("Loading timeline...")
	}

	headerStyle := lipgloss.NewStyle().
		Foreground(activeTheme.Primary).
		Bold(true)
	dateStyle := lipgloss.NewStyle().
		Foreground(activeTheme.Muted).
		Bold(true)
	activeDate := lipgloss.NewStyle().
		Foreground(activeTheme.Primary).
		Bold(true)
	noteStyle := lipgloss.NewStyle().
		Foreground(activeTheme.Fg)
	selectedNote := lipgloss.NewStyle().
		Foreground(activeTheme.Primary).
		Bold(true)
	dimStyle := lipgloss.NewStyle().
		Foreground(activeTheme.Dim)
	todayDot := lipgloss.NewStyle().
		Foreground(activeTheme.Primary).
		SetString("*")

	var b strings.Builder

	title := fmt.Sprintf("Timeline (%s)", m.sortMode)
	b.WriteString(headerStyle.Render(title))
	b.WriteString("  ")
	b.WriteString(dimStyle.Render("[/] dates  j/k notes  s toggle  enter open  q back"))
	b.WriteString("\n\n")

	if len(m.groups) == 0 {
		b.WriteString(dimStyle.Render("No notes yet"))
		return b.String()
	}

	// Show dates as a horizontal navigator.
	maxDates := 5
	startDate := m.groupIdx - maxDates/2
	if startDate < 0 {
		startDate = 0
	}
	endDate := startDate + maxDates
	if endDate > len(m.groups) {
		endDate = len(m.groups)
		startDate = endDate - maxDates
		if startDate < 0 {
			startDate = 0
		}
	}

	for i := startDate; i < endDate; i++ {
		g := m.groups[i]
		label := g.display
		if g.isToday {
			label = todayDot.Render() + " " + label
		}
		if i == m.groupIdx {
			b.WriteString(activeDate.Render("[ " + label + " ]"))
		} else {
			b.WriteString(dateStyle.Render("  " + label + "  "))
		}
	}
	b.WriteString("\n")
	b.WriteString(dimStyle.Render(strings.Repeat("-", m.width)))
	b.WriteString("\n\n")

	if m.groupIdx < len(m.groups) {
		g := m.groups[m.groupIdx]
		// Calculate viewport for scrolling long note lists.
		// Reserve lines for header (3) + date nav (2) + separator (2) + error (1).
		maxVisible := m.height - 8
		if maxVisible < 3 {
			maxVisible = 3
		}
		startIdx := 0
		if m.noteIdx >= maxVisible {
			startIdx = m.noteIdx - maxVisible + 1
		}
		endIdx := startIdx + maxVisible
		if endIdx > len(g.notes) {
			endIdx = len(g.notes)
		}

		for i := startIdx; i < endIdx; i++ {
			n := g.notes[i]
			prefix := "  "
			style := noteStyle
			if i == m.noteIdx {
				prefix = "> "
				style = selectedNote
			}

			tags := ""
			if len(n.Tags) > 0 {
				tags = " " + dimStyle.Render("#"+strings.Join(n.Tags, " #"))
			}

			b.WriteString(prefix + style.Render(n.Title) + tags + "\n")
		}

		if endIdx < len(g.notes) {
			b.WriteString(dimStyle.Render(fmt.Sprintf("  ... %d more", len(g.notes)-endIdx)) + "\n")
		}
	}

	if m.err != "" {
		b.WriteString("\n" + styles.Error.Render(m.err))
	}

	return b.String()
}
