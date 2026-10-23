package emailinbox

import (
	"context"
	"net/netip"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/emailaddress"
	em "vozko/domain/emailinbox"
	"vozko/domain/emailsending"
	"vozko/domain/emailtoken"
	"vozko/domain/lead"
	"vozko/domain/shared"
	balance_usecase "vozko/usecases/balance"
	conversation_usecase "vozko/usecases/conversation"
)

type Clock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

type RawMessages interface {
	Fetch(ctx context.Context, objectKey string) ([]byte, error)
	Delete(ctx context.Context, objectKey string) error
}

type AddressCodec interface {
	OpenAddress(address emailaddress.Address) (emailtoken.Purpose, string, error)
}

type SentMessages interface {
	Get(ctx context.Context, id string) (*emailsending.Message, error)
}

type ReplyDomains interface {
	GetByReplyDomain(ctx context.Context, replyDomain string) (*emailsending.SendingDomain, error)
}

type Senders interface {
	Get(ctx context.Context, workspaceID, id string) (*emailsending.Sender, error)
	ListByWorkspace(ctx context.Context, workspaceID string) ([]emailsending.Sender, error)
}

type SendingDomains interface {
	Get(ctx context.Context, workspaceID, id string) (*emailsending.SendingDomain, error)
}

type SenderAccounts interface {
	GetByWorkspace(ctx context.Context, workspaceID string) (*emailsending.SenderAccount, error)
}

type Suppressions interface {
	Suppressed(ctx context.Context, workspaceID string, addresses []emailaddress.Address) (map[emailaddress.Address]emailsending.SuppressReason, error)
}

type MailtoUnsubscriber interface {
	UnsubscribeMailto(ctx context.Context, messageID string) error
}

type MessageBilling interface {
	ChargeMessage(ctx context.Context, workspaceID, messageKey string) (balance_usecase.EmailQuote, error)
	RefundMessage(ctx context.Context, workspaceID, messageKey string, charge balance_usecase.EmailQuote) error
}

type ReplyBilling interface {
	ChargeMessageWithAttachments(ctx context.Context, workspaceID, messageKey string, encodedAttachmentBytes int64) (balance_usecase.EmailQuote, error)
	RefundMessage(ctx context.Context, workspaceID, messageKey string, charge balance_usecase.EmailQuote) error
}

type Gateway interface {
	Send(ctx context.Context, req emailsending.SendRequest, compose emailsending.ComposeFunc) (*emailsending.Message, error)
}

type LeadFinder interface {
	FindByEmail(workspaceID, email string) (*lead.Lead, error)
}

type MessageStore interface {
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
	Update(messageID string, message *conversation.Message) error
}

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type InboundDispatcher interface {
	Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error
}

type InboundClaims interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
}

type ReceiveBilling interface {
	ChargeReceive(ctx context.Context, workspaceID, messageKey string, sizeBytes int64) (balance_usecase.EmailReceiveCharge, error)
}

type SentOriginals interface {
	Original(ctx context.Context, sent emailsending.Message) (em.SentOriginal, bool, error)
}

type OriginalTranscript interface {
	Holds(conversationID, externalID string) (bool, error)
	TranscribeInto(ctx context.Context, conv em.Conversation, from string, e em.OutboundEmail) error
}

type InboundGuard interface {
	RecordSender(ctx context.Context, ip netip.Addr)
	Pause(ctx context.Context, workspaceID string) error
}
