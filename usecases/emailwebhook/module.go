package emailwebhook_usecase

import (
	"context"
	"crypto/rand"
	"errors"
	"io"
	"log"
	"time"

	"github.com/google/uuid"

	"vozko/domain/emailapi"
	es "vozko/domain/emailsending"
	wh "vozko/domain/emailwebhook"
	"vozko/domain/messaging"
	"vozko/domain/notification"
)

const (
	TopicFanout   = "email_api.webhook_fanout"
	TopicDelivery = "email_api.webhook_delivery"
	WebhooksPath  = "/dashboard/email/api/webhooks"
	fanoutRetries = 5
)

var ErrIncomplete = errors.New("emailwebhook: a required dependency is missing")

type StatusWriter interface {
	MarkStatus(ctx context.Context, messageID string, status emailapi.MessageStatus, event string, at time.Time) (bool, error)
}

type EntryReader interface {
	GetByMessageID(ctx context.Context, messageID string) (*emailapi.LogEntry, error)
}

type Deps struct {
	Endpoints  wh.EndpointStore
	Deliveries wh.DeliveryStore
	Statuses   StatusWriter
	Entries    EntryReader
	Cipher     wh.Cipher
	Hosts      wh.HostChecker
	Poster     wh.Poster
	Slots      wh.Slots
	Queue      messaging.MessageQueuePub
	Alerts     notification.EpisodePublisher
	UserAgent  string
	Now        func() time.Time
	NewID      func() string
	Random     io.Reader
	Logf       func(format string, args ...any)
}

type Module struct {
	d Deps
}

func New(d Deps) (*Module, error) {
	if d.Endpoints == nil || d.Deliveries == nil || d.Statuses == nil || d.Entries == nil || d.Cipher == nil || d.Hosts == nil ||
		d.Poster == nil || d.Slots == nil || d.Queue == nil || d.Alerts == nil || d.UserAgent == "" {
		return nil, ErrIncomplete
	}
	if d.Now == nil {
		d.Now = func() time.Time { return time.Now().UTC() }
	}
	if d.NewID == nil {
		d.NewID = uuid.NewString
	}
	if d.Random == nil {
		d.Random = rand.Reader
	}
	if d.Logf == nil {
		d.Logf = log.Printf
	}
	return &Module{d: d}, nil
}

type FanoutMessage struct {
	MessageID  string        `json:"message_id"`
	Kind       es.EventKind  `json:"kind"`
	OccurredAt time.Time     `json:"occurred_at"`
	Link       string        `json:"link,omitempty"`
	Bounce     *es.Bounce    `json:"bounce,omitempty"`
	Complaint  *es.Complaint `json:"complaint,omitempty"`
	Attempt    int           `json:"attempt"`
}

func (f FanoutMessage) event() es.DeliveryEvent {
	return es.DeliveryEvent{Kind: f.Kind, OccurredAt: f.OccurredAt, Link: f.Link, Bounce: f.Bounce, Complaint: f.Complaint}
}

type DeliveryMessage struct {
	DeliveryID string `json:"delivery_id"`
	EndpointID string `json:"endpoint_id"`
	Attempt    int    `json:"attempt"`
}
