package ws

// WebSocket message type constants.
const (
	MsgTypeAuth            = "auth"
	MsgTypeNoteChanged     = "note.changed"
	MsgTypeTaskProgress    = "task.progress"
	MsgTypeTaskComplete    = "task.complete"
	MsgTypeTaskFailed      = "task.failed"
	MsgTypeChatAsk         = "chat.ask"
	MsgTypeChatStream      = "chat.stream"
	MsgTypeChatDone        = "chat.done"
	MsgTypeLinkSuggestions = "note.link_suggestions"
)

// AuthPayload is the payload for an auth message.
type AuthPayload struct {
	Token string `json:"token"`
}

// NoteChangedPayload is the payload for a note.changed message.
type NoteChangedPayload struct {
	NoteID     string `json:"note_id"`
	ChangeType string `json:"change_type"` // "created", "modified", "deleted"
	UserID     string `json:"-"`           // not sent to client
}
