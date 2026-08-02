package telegram

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// AccountRepository persists connected Telegram bots.
type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	// UpdateWebhookHealth records the result of a getWebhookInfo probe. It is the
	// data-loss alarm's write path, so it is a dedicated narrow update rather
	// than a full-row save that could clobber a concurrent config change.
	UpdateWebhookHealth(ctx context.Context, id string, h WebhookHealth) error
	// SetWebhookRegistered stamps a successful setWebhook.
	SetWebhookRegistered(ctx context.Context, id string, at time.Time) error

	FindByID(ctx context.Context, id string) (*Account, error)
	// FindByIDForWebhook resolves the tenant for an inbound request. It is the
	// hot path, and it must include accounts in every non-revoked status: a
	// webhook-failing account is exactly the one whose next delivery matters.
	FindByIDForWebhook(ctx context.Context, id string) (*Account, error)
	// FindByBotUserID resolves a bot by its Telegram id, for the connect flow's
	// upsert.
	FindByBotUserID(ctx context.Context, botUserID int64) (*Account, error)
	// FindByBotUserIDUnscoped includes soft-deleted rows so reconnecting a
	// previously removed bot restores it instead of colliding with the unique
	// index.
	FindByBotUserIDUnscoped(ctx context.Context, botUserID int64) (*Account, error)
	// FindByBusinessConnectionID routes a business-mode webhook to its tenant.
	// The update carries no bot identity, so this is the only way to know whose
	// conversation it is.
	FindByBusinessConnectionID(ctx context.Context, connectionID string) (*Account, error)
	Restore(ctx context.Context, id string) error

	ListByWorkspace(ctx context.Context, input ListAccountsInput) (*shared.PaginatedResult[*Account], error)
	// ListForHealthCheck returns accounts whose webhook has not been probed
	// recently, oldest first, so the cron makes progress even under a cap.
	ListForHealthCheck(ctx context.Context, before time.Time, limit int) ([]*Account, error)

	Delete(ctx context.Context, id string) error
}

// WebhookHealth is one getWebhookInfo probe result.
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

// ContactRepository persists the people who message our bots.
type ContactRepository interface {
	// FindOrCreate resolves a contact by (account, telegram user id), creating it
	// on first contact.
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	// FindByIDs batch-loads contacts for one page of inbox entries, so hydrating
	// sender identity costs one query rather than one per conversation.
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByTGUserID(ctx context.Context, accountID string, tgUserID int64) (*Contact, error)

	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error
	// SetPhone records a consented phone share and the lead it resolved to.
	SetPhone(ctx context.Context, id, phone string, leadID *string, at time.Time) error
	// UpdateChatID rewrites the chat id after a group→supergroup migration.
	UpdateChatID(ctx context.Context, id string, chatID int64) error
}

type FindOrCreateContactInput struct {
	WorkspaceID string
	AccountID   string
	TGUserID    int64
	TGChatID    int64
	ChatType    string
	// The profile fields Telegram already put in the update, so a first message
	// yields a named contact without an extra API call.
	Username     string
	FirstName    string
	LastName     string
	LanguageCode string
	IsPremium    bool
}

// ContactProfile is the mutable, refreshable part of a contact.
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

// ConversationRepository persists Telegram conversation entries.
type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByContact(ctx context.Context, accountID, contactID string) (*Conversation, error)
	// FindByChat resolves a conversation straight from a chat id, which is what
	// business-mode deletions carry instead of a contact.
	FindByChat(ctx context.Context, accountID string, chatID int64) (*Conversation, error)

	// WorkspaceIDForEntry and DepartmentIDForEntry back the conversation
	// authorizer and the workspace/department resolvers.
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	// RecordInbound advances the customer clock — the anchor for the
	// business-mode 24h window and for inbox ordering.
	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error
	// StatusForEntry reads just the conversation status.
	//
	// It exists so the conversation-status service can be wired with a method
	// reference instead of a closure that loads the whole conversation in the
	// composition root. Reading one column is repository work; the container's
	// job is to connect things, not to query.
	StatusForEntry(ctx context.Context, id string) (string, error)
	SetStartPayload(ctx context.Context, id, payload string) error
	UpdateChatID(ctx context.Context, id string, chatID int64) error

	// CountByStatus powers the inbox status chips, per container or per
	// workspace. Without it a channel's conversations are simply absent from the
	// counts, which reads as "there is no work here".
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

// DeepLinkRepository stores t.me start-payload attribution.
type DeepLinkRepository interface {
	Create(ctx context.Context, d *DeepLink) error
	FindByToken(ctx context.Context, token string) (*DeepLink, error)
	ListByAccount(ctx context.Context, accountID string, limit int) ([]*DeepLink, error)
	// MarkUsed records a redemption. Links are reusable by design (a QR code on a
	// printed invoice is scanned many times), so this counts rather than
	// consumes.
	MarkUsed(ctx context.Context, token string, at time.Time) error
	Delete(ctx context.Context, accountID, token string) error
}

// FileCacheRepository maps our stored objects to Telegram file ids.
//
// "There are no limits for files sent this way", so a cached id turns every
// repeat send of the same asset into a free, instant call. The id is per bot —
// "file_id is unique for each individual bot and can't be transferred from one
// bot to another" — so the cache is keyed by account.
type FileCacheRepository interface {
	Get(ctx context.Context, accountID, sourceKey string) (string, error)
	Put(ctx context.Context, accountID, sourceKey, fileID string) error
}

// ProcessedEventRepository is the durable webhook dedup store, shared in shape
// with Instagram's so both channels get the same at-least-once guarantees.
type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
