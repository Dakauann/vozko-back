package webchat

import "context"

type EventKind string

const (
	EventMessage EventKind = "message"
	EventTyping  EventKind = "typing"
	EventStatus  EventKind = "status"
)

type ConversationState string

const (
	StateOpen    ConversationState = "open"
	StateClosed  ConversationState = "closed"
	StateHuman   ConversationState = "human"
	StateBlocked ConversationState = "blocked"
)

type VisitorEvent struct {
	VisitorID string            `json:"-"`
	Kind      EventKind         `json:"kind"`
	Message   *VisitorMessage   `json:"message,omitempty"`
	Author    Author            `json:"author,omitempty"`
	Typing    bool              `json:"typing,omitempty"`
	State     ConversationState `json:"state,omitempty"`
}

type EventPublisher interface {
	Publish(ctx context.Context, event VisitorEvent) error
}

type EventStream interface {
	Subscribe(visitorID string) (events <-chan VisitorEvent, cancel func(), err error)
}
