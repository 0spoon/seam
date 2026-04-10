package ws

// WebSocket message type constants.
const (
	MsgTypeAuth             = "auth"
	MsgTypeNoteChanged      = "note.changed"
	MsgTypeTaskProgress     = "task.progress"
	MsgTypeTaskComplete     = "task.complete"
	MsgTypeTaskFailed       = "task.failed"
	MsgTypeChatAsk          = "chat.ask"
	MsgTypeChatStream       = "chat.stream"
	MsgTypeChatDone         = "chat.done"
	MsgTypeLinkSuggestions  = "note.link_suggestions"
	MsgTypeWebhookDelivery  = "webhook.delivery"
	MsgTypeAssistantStream  = "assistant.stream"
	MsgTypeAssistantToolUse = "assistant.tool_use"
	MsgTypeAssistantDone    = "assistant.done"
	MsgTypeAssistantError   = "assistant.error"
	MsgTypeLibrarianAction  = "librarian.action"
)

// AuthPayload is the payload for an auth message.
type AuthPayload struct {
	Token string `json:"token"`
}

// LibrarianActionPayload is the payload for a librarian.action message.
type LibrarianActionPayload struct {
	NoteID    string   `json:"note_id"`
	NoteTitle string   `json:"note_title"`
	Actions   []string `json:"actions"`
}

// NoteChangedPayload is the payload for a note.changed message.
type NoteChangedPayload struct {
	NoteID     string `json:"note_id"`
	ChangeType string `json:"change_type"` // "created", "modified", "deleted"
	UserID     string `json:"-"`           // not sent to client
}
