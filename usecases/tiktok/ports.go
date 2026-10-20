package tiktok

import (
	"context"
	"errors"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
	ttdomain "vozko/domain/tiktok"
	conversation_usecase "vozko/usecases/conversation"
)

var (
	ErrUnknownAccount         = errors.New("tiktok: event for an account that is not connected here")
	ErrTokenRefreshInProgress = errors.New("tiktok: token refresh in progress, retry")
	ErrAuthorizationDeclined  = errors.New("tiktok: the account declined the authorization")
	ErrAuthorizationCode      = errors.New("tiktok: the authorization code is missing")
	ErrProbeInconclusive      = errors.New("tiktok: the permission probe could not reach tiktok")
	ErrAccountUnavailable     = errors.New("tiktok: account is not connected or lacks the capability")
)

type Clock func() time.Time

func utcNow() time.Time { return time.Now().UTC() }

type Services struct {
	OAuth     ttdomain.OAuthService
	Profiles  ttdomain.ProfileService
	Messaging ttdomain.MessagingService
	Videos    ttdomain.VideoService
	Comments  ttdomain.CommentService
	Webhooks  ttdomain.WebhookConfigService
	Errors    ttdomain.ErrorClassifier
}

type Repositories struct {
	Accounts      ttdomain.AccountRepository
	Contacts      ttdomain.ContactRepository
	Conversations ttdomain.ConversationRepository
	Videos        ttdomain.VideoRepository
	Comments      ttdomain.CommentRepository
	Purge         ttdomain.PurgeRepository
}

type AccountAlerts interface {
	NeedsReconnect(ctx context.Context, account *ttdomain.Account) error
	ReconnectSoon(ctx context.Context, account *ttdomain.Account) error
	WebhooksUnhealthy(ctx context.Context, account *ttdomain.Account) error
	Healthy(ctx context.Context, account *ttdomain.Account) error
}

type Metrics interface {
	AddTikTokEvent(stage, outcome string)
}

type AssignmentService interface {
	EnsureAssignment(entryID, entryType, accountID string) string
}

type InboundDispatcher interface {
	Dispatch(ctx context.Context, in conversation_usecase.InboundAutomationInput) error
}

type MessageStore interface {
	GetByEntryAndExternalMessageID(entryType shared.EntryType, entryID, externalID string) (*conversation.Message, error)
	Update(messageID string, message *conversation.Message) error
}

type ObjectStore interface {
	UploadFile(key string, data []byte, contentType string) error
	DeleteFile(ctx context.Context, key string) error
	GetFileURL(key string) string
}

type MediaFetcher interface {
	Fetch(ctx context.Context, url string) ([]byte, error)
}
