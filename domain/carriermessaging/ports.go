package carriermessaging

import (
	"context"
	"errors"
	"time"
	"vozko/domain/rcs"
	"vozko/domain/sms"
)

var (
	ErrProviderRefused     = errors.New("rcs: the provider refused the request")
	ErrProviderUnavailable = errors.New("rcs: the provider could not be reached")
	ErrSendOutcomeUnknown  = errors.New("rcs: the provider may have taken the message, its answer was lost")
)

type SendRequest struct {
	Reference       string
	SenderProfileID string
	Phone           string
	Content         rcs.Content
	Validity        time.Duration
}

type SendOutcome struct {
	Accepted    bool
	ErrorCode   string
	Detail      string
	Unreachable bool
}

type SenderSubmission struct {
	RequestID string
	ProfileID string
}

type SenderStatusReport struct {
	State  rcs.ProviderState
	Reason string
}

type Provider interface {
	Name() string
	Send(ctx context.Context, req SendRequest) (SendOutcome, error)
	SubmitSender(ctx context.Context, s rcs.Sender) (SenderSubmission, error)
	SenderStatus(ctx context.Context, s rcs.Sender) (SenderStatusReport, error)
}

type SMSRequest struct {
	Reference string
	From      string
	Phone     string
	Text      string
}

type SMSProvider interface {
	SendSMS(ctx context.Context, req SMSRequest) (SendOutcome, error)
}

type SenderRepository interface {
	Create(ctx context.Context, s *rcs.Sender) error
	Update(ctx context.Context, s *rcs.Sender) error
	FindByID(ctx context.Context, workspaceID, id string) (*rcs.Sender, error)
	FindByIDUnscoped(ctx context.Context, id string) (*rcs.Sender, error)
	FindByProfileID(ctx context.Context, provider, profileID string) (*rcs.Sender, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*rcs.Sender, error)
	ListAwaitingProvider(ctx context.Context, limit int) ([]*rcs.Sender, error)
	ListAll(ctx context.Context, status *rcs.SenderStatus) ([]*rcs.Sender, error)
	Delete(ctx context.Context, workspaceID, id string) error
}

type MessageRepository interface {
	Create(ctx context.Context, m *Message) error
	FindByID(ctx context.Context, id string) (*Message, error)
	FindByReference(ctx context.Context, reference string) (*Message, error)
	Advance(ctx context.Context, reference string, next Status, errorCode string, at time.Time) (*Message, bool, error)
	LinkFallback(ctx context.Context, messageID, fallbackMessageID string) (bool, error)
	ListOverdue(ctx context.Context, now time.Time, limit int) ([]*Message, error)
	CountMarketingSince(ctx context.Context, workspaceID, phone string, since time.Time) (int, error)
	CountRunKindSince(ctx context.Context, workspaceID string, kind RunKind, since time.Time) (int, error)
	LatestDeliveredTo(ctx context.Context, workspaceID, phone string, since time.Time) (*Message, error)
	LatestSMSTo(ctx context.Context, phone string, since time.Time) (*Message, error)
}

type EventRepository interface {
	Record(ctx context.Context, e *MessageEvent) (bool, error)
	CountSince(ctx context.Context, workspaceID, phone string, kind EventKind, since time.Time) (int, error)
}

type SuppressionRepository interface {
	Add(ctx context.Context, s Suppression) (bool, error)
	Remove(ctx context.Context, workspaceID, phone string) error
	IsSuppressed(ctx context.Context, workspaceID, phone string) (bool, error)
	SuppressedAmong(ctx context.Context, workspaceID string, phones []string) (map[string]bool, error)
	List(ctx context.Context, workspaceID string, limit, offset int) ([]Suppression, int, error)
}

type SettingsRepository interface {
	Get(ctx context.Context, workspaceID string) (*Settings, error)
	Put(ctx context.Context, s Settings) error
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, phone, displayName string) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	SetLead(ctx context.Context, id, leadID string) error
	MarkFlooding(ctx context.Context, id string, at time.Time) (bool, error)
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, senderID, contactID string) (*Conversation, bool, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Conversation, error)
	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	OpenBilledConversation(ctx context.Context, id string, at time.Time) (bool, error)
}

type RouteRepository interface {
	Create(ctx context.Context, r *sms.Route) error
	Update(ctx context.Context, r *sms.Route) error
	FindByID(ctx context.Context, workspaceID, id string) (*sms.Route, error)
	FindByIDUnscoped(ctx context.Context, id string) (*sms.Route, error)
	FindByNumber(ctx context.Context, number string) (*sms.Route, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]*sms.Route, error)
	ListAll(ctx context.Context) ([]*sms.Route, error)
	Delete(ctx context.Context, workspaceID, id string) error
}
