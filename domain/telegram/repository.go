package telegram

import (
	"context"
	"time"
	"vozko/domain/conversation"

	"vozko/domain/shared"
)

type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateWebhookHealth(ctx context.Context, id string, h WebhookHealth) error
	SetWebhookRegistered(ctx context.Context, id string, at time.Time) error

	FindByID(ctx context.Context, id string) (*Account, error)
	FindByIDForWebhook(ctx context.Context, id string) (*Account, error)
	FindByBotUserID(ctx context.Context, botUserID int64) (*Account, error)
	FindByBotUserIDUnscoped(ctx context.Context, botUserID int64) (*Account, error)
	FindByBusinessConnectionID(ctx context.Context, connectionID string) (*Account, error)
	Restore(ctx context.Context, id string) error

	ListByWorkspace(ctx context.Context, input ListAccountsInput) (*shared.PaginatedResult[*Account], error)
	ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*Account, error)

	Delete(ctx context.Context, id string) error
}

type WebhookHealth struct {
	PendingCount int
	LastError    string
	LastErrorAt  *time.Time
	CheckedAt    time.Time
}

type ListAccountsInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Mode        *Mode
	Options     shared.QueryOptions
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByTGUserID(ctx context.Context, accountID string, tgUserID int64) (*Contact, error)

	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error
	SetPhone(ctx context.Context, id, phone string, leadID *string, at time.Time) error
	UpdateChatID(ctx context.Context, id string, chatID int64) error
}

type FindOrCreateContactInput struct {
	WorkspaceID  string
	AccountID    string
	TGUserID     int64
	TGChatID     int64
	ChatType     string
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
	IsPremium    bool
}

type ContactProfile struct {
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
	IsPremium    bool
	PhotoFileID  string
	PhotoURL     string
	FetchedAt    time.Time
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByContact(ctx context.Context, accountID, contactID string) (*Conversation, error)
	FindByChat(ctx context.Context, accountID string, chatID int64) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)
	SetStartPayload(ctx context.Context, id, payload string) error
	UpdateChatID(ctx context.Context, id string, chatID int64) error

	CountByStatus(ctx context.Context, workspaceID, accountID string) (map[string]int64, error)
}

type FindOrCreateConversationInput struct {
	WorkspaceID          string
	AccountID            string
	ContactID            string
	TGChatID             int64
	ChatType             string
	BusinessConnectionID *string
}

type DeepLinkRepository interface {
	Create(ctx context.Context, d *DeepLink) error
	FindByToken(ctx context.Context, token string) (*DeepLink, error)
	ListByAccount(ctx context.Context, accountID string, limit int) ([]*DeepLink, error)
	MarkUsed(ctx context.Context, token string, at time.Time) error
	Delete(ctx context.Context, accountID, token string) error
}

type FileCacheRepository interface {
	Get(ctx context.Context, accountID, sourceKey string) (string, error)
	Put(ctx context.Context, accountID, sourceKey, fileID string) error
}

type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
