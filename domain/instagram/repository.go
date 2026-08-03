package instagram

import (
	"context"
	"time"

	"vozko/domain/shared"
)

// AccountRepository persists connected Instagram accounts.
type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	// UpdateToken rotates credentials without touching the rest of the row.
	UpdateToken(ctx context.Context, id, token string, expiresAt, refreshedAt time.Time) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateMessagingHealth(ctx context.Context, id string, healthy bool, checkedAt time.Time) error
	SetWebhookSubscribedAt(ctx context.Context, id string, at time.Time) error

	FindByID(ctx context.Context, id string) (*Account, error)
	// FindByIGUserID resolves the account a webhook belongs to. This is the hot
	// path for inbound events, so it is a single indexed lookup.
	FindByIGUserID(ctx context.Context, igUserID string) (*Account, error)
	// FindByIGUserIDUnscoped includes soft-deleted rows so re-onboarding a
	// previously disconnected account restores it instead of colliding with the
	// unique index.
	FindByIGUserIDUnscoped(ctx context.Context, igUserID string) (*Account, error)
	Restore(ctx context.Context, id string) error

	ListByWorkspace(ctx context.Context, input ListAccountsInput) (*shared.PaginatedResult[*Account], error)
	// ListDueForTokenRefresh returns connected accounts whose token is nearing
	// expiry and is old enough to be refreshed (Instagram rejects a refresh on a
	// token younger than 24h).
	ListDueForTokenRefresh(ctx context.Context, before time.Time, limit int) ([]*Account, error)

	Delete(ctx context.Context, id string) error
}

type ListAccountsInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Options     shared.QueryOptions
}

// ContactRepository persists the people who message our accounts.
type ContactRepository interface {
	// FindOrCreate resolves a contact by (account, IGSID), creating it on first
	// contact. Identity is account-scoped because an IGSID is only unique within
	// the (app, professional account) pair.
	FindOrCreate(ctx context.Context, workspaceID, igAccountID, igsid string) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	// FindByIDs batch-loads contacts for one page of inbox entries, so hydrating
	// sender identity costs one query rather than one per conversation.
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByIGSID(ctx context.Context, igAccountID, igsid string) (*Contact, error)
	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool) error
}

// ContactProfile is the mutable, refreshable part of a contact.
type ContactProfile struct {
	Username             string
	Name                 string
	ProfilePictureURL    string
	IsVerifiedUser       bool
	FollowerCount        int
	IsUserFollowBusiness bool
	IsBusinessFollowUser bool
	FetchedAt            time.Time
}

// ConversationRepository persists Instagram conversation entries.
type ConversationRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, igAccountID, contactID string) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByContact(ctx context.Context, igAccountID, contactID string) (*Conversation, error)

	// WorkspaceIDForEntry and DepartmentIDForEntry back the conversation
	// authorizer and the workspace/department resolvers, which are keyed on
	// (entry_id, entry_type) and therefore need one lookup per channel.
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	// ListEntryIDsByWorkspace enumerates a workspace's conversations for the
	// authorizer's accessible-entry check.
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	// RecordInbound advances the customer clock, the anchor for the 24h window.
	RecordInbound(ctx context.Context, id string, at time.Time) error
	// RecordOutbound advances the agent clock.
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetIGConversationID(ctx context.Context, id, igConversationID string) error
	SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error
	// SetAutomationEnabled is the per-conversation automation override an
	// operator flips when taking a conversation over.
	//
	// A nil value clears the override so the conversation inherits the account
	// switch again, which is a different state from an explicit false, and the
	// gating in the webhook handlers reads it that way.
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	// CountByStatus powers the inbox status chips, per account or per workspace.
	// Without it the channel's conversations are absent from the counts, which
	// reads as "there is no work here" while the list below shows work.
	CountByStatus(ctx context.Context, workspaceID, igAccountID string) (map[string]int64, error)
	// StatusForEntry reads just the conversation status.
	//
	// It exists so the conversation-status service can be wired with a method
	// reference instead of a closure that loads the whole conversation in the
	// composition root. Reading one column is repository work; the container's
	// job is to connect things, not to query.
	StatusForEntry(ctx context.Context, id string) (string, error)
}

// MediaRepository stores the durable projection of a post. CDN URLs are never
// persisted here.
type MediaRepository interface {
	Upsert(ctx context.Context, m *Media) error
	UpsertMany(ctx context.Context, items []*Media) error
	FindByIGMediaID(ctx context.Context, igAccountID, igMediaID string) (*Media, error)
	UpdateCounts(ctx context.Context, igAccountID, igMediaID string, likeCount, commentsCount int) error
	SetCommentEnabled(ctx context.Context, igAccountID, igMediaID string, enabled bool) error
}

// CommentRepository stores comments so the moderation queue is push-driven.
// The Graph comments edge cannot be filtered by timestamp, so incremental sync
// has to come from webhooks.
type CommentRepository interface {
	Upsert(ctx context.Context, c *Comment) error
	UpsertMany(ctx context.Context, items []*Comment) error
	FindByIGCommentID(ctx context.Context, igAccountID, igCommentID string) (*Comment, error)
	SetHidden(ctx context.Context, igAccountID, igCommentID string, hidden bool) error
	Delete(ctx context.Context, igAccountID, igCommentID string) error
	ListByMedia(ctx context.Context, input ListCommentsInput) (*shared.PaginatedResult[*Comment], error)
}

type ListCommentsInput struct {
	IGAccountID string
	IGMediaID   string
	// TopLevelOnly excludes replies, matching the Graph edge's default.
	TopLevelOnly bool
	HiddenOnly   *bool
	Options      shared.QueryOptions
}

// PrivateReplyRepository guards Instagram's one-private-reply-per-comment rule.
type PrivateReplyRepository interface {
	// Claim atomically reserves the single allowance for a comment. It returns
	// false when a reply was already attempted or sent, in which case the caller
	// must NOT issue the HTTP request.
	Claim(ctx context.Context, igCommentID, igAccountID string) (claimed bool, err error)
	MarkSent(ctx context.Context, igCommentID, recipientIGSID, igMessageID string) error
	MarkFailed(ctx context.Context, igCommentID string, code int, message string) error
	Find(ctx context.Context, igCommentID string) (*PrivateReply, error)
}

// ProcessedEventRepository is the durable webhook dedup store.
//
// The existing WhatsApp pipeline dedups only in Redis with a 5-minute TTL, and
// its one-shot variant fails OPEN on a Redis error. A Postgres table backed by
// INSERT ... ON CONFLICT DO NOTHING survives eviction and restarts, which is
// what at-least-once delivery actually requires.
type ProcessedEventRepository interface {
	// Claim inserts the idempotency key, returning false when it already exists.
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
	// PurgeOlderThan trims the table; called from cron.
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
