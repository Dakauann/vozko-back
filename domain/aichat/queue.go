package aichat

import (
	"errors"
	"time"
)

const MaxClientIDLen = 64

var (
	ErrInvalidClientID   = errors.New("aichat: invalid client message id")
	ErrQueuedMessageGone = errors.New("aichat: the queued message was already delivered or removed")
)

type QueuedMessage struct {
	ID          string
	ThreadID    string
	ClientID    string
	Content     string
	Attachments []byte
	MessageID   string
	CreatedAt   time.Time
	DeliveredAt *time.Time
}

func (q QueuedMessage) Delivered() bool {
	return q.DeliveredAt != nil
}

func (q QueuedMessage) Message() *Message {
	return &Message{ThreadID: q.ThreadID, Role: RoleUser, Content: q.Content, Attachments: q.Attachments}
}

func ValidClientID(id string) error {
	if id == "" || len(id) > MaxClientIDLen {
		return ErrInvalidClientID
	}
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-', r == '_':
		default:
			return ErrInvalidClientID
		}
	}
	return nil
}

type QueueRepository interface {
	Enqueue(message *QueuedMessage) error
	Pending(threadID string) ([]*QueuedMessage, error)
	Remove(threadID, clientID string) error
	Deliver(threadID string) ([]*QueuedMessage, error)
}
