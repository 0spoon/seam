// Package review provides the knowledge gardening review queue that
// aggregates orphan notes, untagged notes, inbox notes, and similar
// pairs into a prioritized list for the user to triage.
package review

// ReviewItem represents a single item in the review queue.
type ReviewItem struct {
	Type        string       `json:"type"` // "orphan", "untagged", "inbox", "similar_pair"
	NoteID      string       `json:"note_id"`
	NoteTitle   string       `json:"note_title"`
	NoteSnippet string       `json:"note_snippet"`
	Suggestions []Suggestion `json:"suggestions"`
	CreatedAt   string       `json:"created_at"`
}

// Suggestion represents a possible action for a review item.
type Suggestion struct {
	Action string `json:"action"` // "add_tag", "move_project", "add_link"
	Target string `json:"target"` // tag name, project id, or note id
	Reason string `json:"reason"`
}
