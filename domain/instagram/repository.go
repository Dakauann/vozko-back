package instagram

import (
	"context"
	"time"

	"vozko/domain/shared"
)

type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	UpdateToken(ctx context.Context, id, token string, expiresAt, refreshedAt time.Time) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateMessagingHealth(ctx context.Context, id string, healthy bool, checkedAt time.Time) error
	SetWebhookSubscribedAt(ctx context.Context, id string, at time.Time) error

	FindByID(ctx context.Context, id string) (*Account, error)
	FindByIGUserID(ctx context.Context, igUserID string) (*Account, error)
	FindByIGUserIDUnscoped(ctx context.Context, igUserID string) (*Account, error)
	Restore(ctx context.Context, id string) error

	ListByWorkspace(ctx context.Context, input ListAccountsInput) (*shared.PaginatedResult[*Account], error)
	ListDueForTokenRefresh(ctx context.Context, before time.Time, limit int) ([]*Account, error)

	Delete(ctx context.Context, id string) error
}

type ListAccountsInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Options     shared.QueryOptions
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, igAccountID, igsid string) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByIGSID(ctx context.Context, igAccountID, igsid string) (*Contact, error)
	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	SetBlocked(ctx context.Context, id string, blocked bool) error
}

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

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, igAccountID, contactID string) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByContact(ctx context.Context, igAccountID, contactID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetIGConversationID(ctx context.Context, id, igConversationID string) error
	SetStatus(ctx context.Context, id, status, closeSource, closeReason string, closedAt *time.Time) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	CountByStatus(ctx context.Context, workspaceID, igAccountID string) (map[string]int64, error)
	StatusForEntry(ctx context.Context, id string) (string, error)
}

type MediaRepository interface {
	Upsert(ctx context.Context, m *Media) error
	UpsertMany(ctx context.Context, items []*Media) error
	FindByIGMediaID(ctx context.Context, igAccountID, igMediaID string) (*Media, error)
	UpdateCounts(ctx context.Context, igAccountID, igMediaID string, likeCount, commentsCount int) error
	SetCommentEnabled(ctx context.Context, igAccountID, igMediaID string, enabled bool) error
	ListByAccount(ctx context.Context, igAccountID string, limit, offset int) ([]*Media, error)
}

type CommentRepository interface {
	Upsert(ctx context.Context, c *Comment) error
	UpsertMany(ctx context.Context, items []*Comment) error
	FindByIGCommentID(ctx context.Context, igAccountID, igCommentID string) (*Comment, error)
	SetHidden(ctx context.Context, igAccountID, igCommentID string, hidden bool) error
	Delete(ctx context.Context, igAccountID, igCommentID string) error
	ListByMedia(ctx context.Context, input ListCommentsInput) (*shared.PaginatedResult[*Comment], error)
}

type ListCommentsInput struct {
	IGAccountID  string
	IGMediaID    string
	TopLevelOnly bool
	HiddenOnly   *bool
	Options      shared.QueryOptions
}

type PrivateReplyRepository interface {
	Claim(ctx context.Context, igCommentID, igAccountID string) (claimed bool, err error)
	MarkSent(ctx context.Context, igCommentID, recipientIGSID, igMessageID string) error
	MarkFailed(ctx context.Context, igCommentID string, code int, message string) error
	Find(ctx context.Context, igCommentID string) (*PrivateReply, error)
}

type ProcessedEventRepository interface {
	Claim(ctx context.Context, key, channel, accountID string) (claimed bool, err error)
	PurgeOlderThan(ctx context.Context, cutoff time.Time) (int64, error)
}
