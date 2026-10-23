package carriermessaging_usecase

import (
	"context"
	"time"

	"vozko/domain/carriermessaging"
	"vozko/domain/conversation"
	"vozko/domain/lead"
	"vozko/domain/rcs"
	"vozko/domain/workflow"
)

type BalanceReader interface {
	GetBalance(workspaceID string) (int64, error)
}

type SpendReader interface {
	SpendSince(ctx context.Context, workspaceID string, since time.Time) (int64, error)
}

type Links interface {
	TrackedURL(messageID string, key rcs.ButtonKey) (string, error)
	Verify(token string) (string, rcs.ButtonKey, error)
}

type LeadResolver interface {
	EnsureLead(ctx context.Context, workspaceID, phone, name string) (*lead.Lead, error)
}

type Transcript interface {
	Record(ctx context.Context, record conversation.MessageHistoryRecord) error
}

type TranscriptStatus interface {
	UpdateDeliveryStatusWithReason(providerMessageID string, status conversation.DeliveryStatus, errorCode int, errorMessage string) error
}

type StatusAnnouncer interface {
	BroadcastMessageStatus(entryID, entryType, messageID string, status conversation.DeliveryStatus)
}

type WorkspaceTimezones interface {
	Timezone(ctx context.Context, workspaceID string) (string, error)
}

type WhatsAppStart struct {
	WorkspaceID     string
	UserID          string
	BusinessPhoneID string
	TemplateID      string
	Phone           string
	Name            string
	BodyParams      []string
	HeaderParams    []string
	IdempotencyKey  string
}

type WhatsAppStarter interface {
	StartTemplate(ctx context.Context, in WhatsAppStart) (string, error)
}

type Repositories struct {
	Senders          carriermessaging.SenderRepository
	Messages         carriermessaging.MessageRepository
	Events           carriermessaging.EventRepository
	Suppressions     carriermessaging.SuppressionRepository
	Settings         carriermessaging.SettingsRepository
	Contacts         carriermessaging.ContactRepository
	RCSConversations carriermessaging.ConversationRepository
	SMSConversations carriermessaging.ConversationRepository
	Routes           carriermessaging.RouteRepository
}

type Notices interface {
	ContactFlooding(ctx context.Context, workspaceID string, channel carriermessaging.Channel, contact carriermessaging.Contact, inboundInWindow int)
	SenderStateChanged(ctx context.Context, sender rcs.Sender)
}

type AutomationInput struct {
	WorkspaceID          string
	Channel              carriermessaging.Channel
	ConversationID       string
	SenderID             string
	ContactRef           string
	LeadID               *string
	ProviderMessageID    string
	Text                 string
	Selection            *workflow.OptionSelection
	ReceivedAt           time.Time
	Config               conversation.ChannelAutomation
	ConversationOverride *bool
}

type Inbox interface {
	EnsureAssignment(ctx context.Context, workspaceID string, channel carriermessaging.Channel, conversationID, senderID string) error
	DispatchAutomation(ctx context.Context, in AutomationInput)
}
