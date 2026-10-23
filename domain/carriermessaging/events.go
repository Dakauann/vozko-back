package carriermessaging

import (
	"time"

	"vozko/domain/rcs"
)

type StatusReport struct {
	Reference   string
	Status      Status
	ErrorCode   string
	ErrorDetail string
	Operator    string
	Unreachable bool
	ReceivedAt  time.Time
}

type InboundMedia struct {
	URL      string
	MIMEType string
	Name     string
}

type Inbound struct {
	ProviderMessageID string
	Phone             string
	ContactName       string
	Channel           Channel
	To                string
	Text              string
	Media             *InboundMedia
	InReplyTo         string
	Postback          string
	ReceivedAt        time.Time
}

type SuggestionClick struct {
	EventID    string
	Reference  string
	Phone      string
	To         string
	Postback   string
	Label      string
	OccurredAt time.Time
}

type EventKind string

const (
	EventInbound EventKind = "inbound"
	EventTap     EventKind = "tap"
	EventClick   EventKind = "click"
	EventOptOut  EventKind = "opt_out"
)

type MessageEvent struct {
	ID          string
	WorkspaceID string
	MessageID   string
	SenderID    string
	Phone       string
	Kind        EventKind
	Button      *rcs.ButtonKey
	Label       string
	DedupKey    string
	OccurredAt  time.Time
}
