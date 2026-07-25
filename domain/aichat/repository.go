package aichat

import (
	"errors"
	"time"
)

// ErrThreadNotFound is returned when a thread does not exist (or is not visible to
// the caller's workspace/user — callers must authorize against the returned thread).
var ErrThreadNotFound = errors.New("aichat: thread not found")

type ListThreadsInput struct {
	WorkspaceID string
	UserID      string
	Limit       int
	Offset      int
}

type ThreadRepository interface {
	Create(thread *Thread) error
	GetByID(id string) (*Thread, error)
	// ListByUser returns the user's threads in this workspace, most-recent first,
	// with the total count for pagination.
	ListByUser(input ListThreadsInput) ([]*Thread, int64, error)
	Rename(id, title string) error
	// Touch updates the thread's last-activity timestamp (and model, when non-empty)
	// after a new turn, so the sidebar can sort by recency.
	Touch(id string, lastMessageAt time.Time, model string) error
	Delete(id string) error
}

type ListMessagesInput struct {
	ThreadID string
	Limit    int
	Offset   int
}

type MessageRepository interface {
	Create(message *Message) error
	// ListByThread returns messages oldest-first (chat reading order) with the total
	// count for pagination.
	ListByThread(input ListMessagesInput) ([]*Message, int64, error)
	DeleteByThread(threadID string) error
}
