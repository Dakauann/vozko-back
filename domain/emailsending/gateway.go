package emailsending

import (
	"context"
	"errors"
	"time"

	"vozko/domain/emailaddress"
)

var (
	ErrRecipientSuppressed = errors.New("emailsending: recipient suppressed")
	ErrNoDefaultSender     = errors.New("emailsending: no default sender")
)

type DailyCapError struct {
	Cap      int64
	ResumeAt time.Time
}

func (e *DailyCapError) Error() string {
	return ErrDailyCapReached.Error()
}

func (e *DailyCapError) Unwrap() error {
	return ErrDailyCapReached
}

type SendRequest struct {
	WorkspaceID string
	SenderID    string
	To          emailaddress.Address
	Kind        MessageKind
	RunKind     RunKind
	RunID       string
	RecipientID string
	InReplyTo   string
	References  []string
	Tags        map[string]string
	ReplyTo     []emailaddress.Address
	Headers     []Header
}

type Envelope struct {
	MessageID      string
	From           Mailbox
	ReplyTo        emailaddress.Address
	UnsubscribeURL string
	Domain         SendingDomain
	Sender         Sender
}

type Content struct {
	Subject     string
	HTML        string
	Text        string
	Attachments []Attachment
}

type ComposeFunc func(Envelope) (Content, error)

type Gateway interface {
	Send(ctx context.Context, req SendRequest, compose ComposeFunc) (*Message, error)
}

type EnvelopeReader interface {
	Envelope(ctx context.Context, req SendRequest, sent Message) (Envelope, error)
}

type Unsubscriber interface {
	UnsubscribeByComplaint(ctx context.Context, workspaceID string, address emailaddress.Address, messageID string) error
}

type MessageObserver interface {
	OnMessageEvent(ctx context.Context, m Message, ev DeliveryEvent, effect EventEffect) error
}

type InboundHandler interface {
	HandleInbound(ctx context.Context, notice InboundNotice) error
}

type SuppressedError struct {
	Reason SuppressReason
}

func (e SuppressedError) Error() string {
	return ErrRecipientSuppressed.Error() + ": " + string(e.Reason)
}

func (e SuppressedError) Is(target error) bool {
	return target == ErrRecipientSuppressed
}

func SuppressReasonOf(err error) SuppressReason {
	var s SuppressedError
	if errors.As(err, &s) {
		return s.Reason
	}
	return ""
}
