package mercadolivre

import (
	"context"
	"errors"
	"time"

	"vozko/domain/conversation"
	mldomain "vozko/domain/mercadolivre"
	"vozko/domain/shared"
	conversation_usecase "vozko/usecases/conversation"
)

var (
	ErrUnknownAccount         = errors.New("mercadolivre: notification for a seller with no live account")
	ErrAccountUnavailable     = errors.New("mercadolivre: account is not connected or lacks the capability")
	ErrJobKindNotHandled      = errors.New("mercadolivre: notification kind is not handled yet")
	ErrTokenRefreshInProgress = errors.New("mercadolivre: token refresh in progress, retry")
	ErrAuthorizationDeclined  = errors.New("mercadolivre: the seller declined the authorization")
	ErrAuthorizationCode      = errors.New("mercadolivre: the authorization code is missing")
	ErrIdentityMismatch       = errors.New("mercadolivre: the token belongs to another seller")
	ErrProbeInconclusive      = errors.New("mercadolivre: the permission probe could not reach Mercado Livre")
)

type Clock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

type Services struct {
	OAuth       mldomain.OAuthService
	Users       mldomain.UserService
	Questions   mldomain.QuestionService
	Messages    mldomain.MessageService
	Attachments mldomain.AttachmentService
	Packs       mldomain.PackService
	Orders      mldomain.OrderService
	Shipments   mldomain.ShipmentService
	MissedFeeds mldomain.MissedFeedService
	Items       mldomain.ItemService
	ActionGuide mldomain.ActionGuideService
	Errors      mldomain.ErrorClassifier
}

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type QueuePublisher interface {
	Publish(topic string, payload []byte) error
}

type AccountAlerts interface {
	NeedsReconnect(ctx context.Context, account *mldomain.Account) error
	Reconnected(ctx context.Context, account *mldomain.Account) error
}

type MessageStore interface {
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
	Update(messageID string, message *conversation.Message) error
}

type InboundDispatcher interface {
	Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error
}

type InboundClaims interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
}

type Metrics interface {
	AddMercadoLivreEvent(stage, outcome string)
}

type JobHandler interface {
	Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error
}

type JobHandlerFunc func(ctx context.Context, account *mldomain.Account, job mldomain.Job) error

func (f JobHandlerFunc) Handle(ctx context.Context, account *mldomain.Account, job mldomain.Job) error {
	return f(ctx, account, job)
}

type AccountReconciler interface {
	Reconcile(ctx context.Context, account *mldomain.Account, creds mldomain.Credentials, marker string) (published int, err error)
}
