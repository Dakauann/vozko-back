package tiktok

import (
	"context"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type TokenUpdate struct {
	AccessToken      string
	RefreshToken     string
	AccessExpiresAt  time.Time
	RefreshExpiresAt time.Time
	GrantedScopes    []string
}

type RefreshOutcome struct {
	Tokens       *TokenUpdate
	Status       Status
	StatusReason string
	PurgeAfter   *time.Time
}

type RefreshFunc func(ctx context.Context, account *Account) (RefreshOutcome, error)

type ProfileUpdate struct {
	Username          string
	DisplayName       string
	AvatarKey         string
	HasAdvancedAccess bool
	IsBusinessAccount bool
	GrantedScopes     []string
	Capabilities      AccountCapabilities
}

type ListAccountsInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Options     shared.QueryOptions
}

type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	Reconnect(ctx context.Context, a *Account) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateProfile(ctx context.Context, id string, p ProfileUpdate) error
	LearnUniqueIdentifier(ctx context.Context, id, uniqueIdentifier string) error
	SetWebhooksHealthy(ctx context.Context, id string, healthy bool) error

	FindByID(ctx context.Context, id string) (*Account, error)
	FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*Account, error)
	FindLiveByOpenID(ctx context.Context, openID string) (*Account, error)
	FindByWorkspaceAndOpenIDUnscoped(ctx context.Context, workspaceID, openID string) (*Account, error)
	Restore(ctx context.Context, id string) error

	ListByWorkspace(ctx context.Context, in ListAccountsInput) (*shared.PaginatedResult[*Account], error)
	ListConnected(ctx context.Context, afterID string, limit int) ([]*Account, error)

	ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*Account, error)
	LockForRefresh(ctx context.Context, id string, refresh RefreshFunc) (acquired bool, err error)

	ListDueForHealthCheck(ctx context.Context, checkedBefore time.Time, limit int) ([]*Account, error)
	MarkHealthChecked(ctx context.Context, id string, at time.Time) error

	Revoke(ctx context.Context, id string, reason string, purgeAfter time.Time) error
	Disconnect(ctx context.Context, id string, status Status, reason string, purgeAfter time.Time) error
	ListDueForPurge(ctx context.Context, now time.Time, limit int) ([]*Account, error)
	DeletePurged(ctx context.Context, id string) error
}

type FindOrCreateContactInput struct {
	WorkspaceID      string
	AccountID        string
	UniqueIdentifier string
	Username         string
	IsFollower       *bool
}

type ContactProfile struct {
	Username    string
	DisplayName string
	AvatarKey   string
	IsFollower  *bool
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByUniqueIdentifier(ctx context.Context, accountID, uniqueIdentifier string) (*Contact, error)
	UpdateProfile(ctx context.Context, id string, p ContactProfile, at time.Time) error
	SetLeadID(ctx context.Context, id string, leadID *string) error
}

type FindOrCreateConversationInput struct {
	WorkspaceID    string
	AccountID      string
	ContactID      string
	ConversationID string
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (conv *Conversation, created bool, err error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Conversation, error)
	FindByConversationID(ctx context.Context, accountID, conversationID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	ClaimOutbound(ctx context.Context, id string, now time.Time) (claimed bool, err error)
	ReleaseOutbound(ctx context.Context, id string) error
	CountOutbound(ctx context.Context, id string) error
	CloseBudget(ctx context.Context, id string, at time.Time) error

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetImageCapability(ctx context.Context, id string, allowed bool, at time.Time) error
	SetReferral(ctx context.Context, id string, referral Referral) error

	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)
	CountByStatus(ctx context.Context, workspaceID, accountID string) (map[string]int64, error)
}

type ListVideosInput struct {
	AccountID string
	Search    string
	Options   shared.QueryOptions
}

type VideoRepository interface {
	UpsertMany(ctx context.Context, videos []*Video) error
	FindByItemID(ctx context.Context, accountID, itemID string) (*Video, error)
	FindByItemIDs(ctx context.Context, accountID string, itemIDs []string) ([]*Video, error)
	ListByAccount(ctx context.Context, in ListVideosInput) (*shared.PaginatedResult[*Video], error)
	ListSlice(ctx context.Context, accountID string, limit, offset int) ([]*Video, error)
}

type CommentEventWrite struct {
	WorkspaceID string
	AccountID   string
	Event       CommentEvent
	OccurredAt  time.Time
	KnownOurs   bool
	ContactID   *string
}

type ResolvedAuthor struct {
	ContactID        *string
	UniqueIdentifier string
	Username         string
	DisplayName      string
	Text             string
	ImageURL         string
	Visibility       CommentVisibility
	IsOurs           bool
	Liked            bool
	Likes            int
	Replies          int
	CommentedAt      *time.Time
}

type ListCommentsInput struct {
	AccountID     string
	VideoID       string
	IncludeHidden bool
	Options       shared.QueryOptions
}

type CommentRepository interface {
	ApplyEvent(ctx context.Context, w CommentEventWrite) (applied bool, err error)
	RecordOurs(ctx context.Context, c *Comment) error
	MirrorMany(ctx context.Context, comments []*Comment) error

	ClaimLookups(ctx context.Context, now time.Time, lease time.Duration, limit int) ([]*Comment, error)
	ResolveLookup(ctx context.Context, id string, author ResolvedAuthor) error
	DeferLookup(ctx context.Context, id string, attempts int, next time.Time) error
	FailLookup(ctx context.Context, id string, attempts int) error
	MarkRulesDone(ctx context.Context, id string) error

	FindByCommentID(ctx context.Context, accountID, commentID string) (*Comment, error)
	FindByCommentIDs(ctx context.Context, accountID string, commentIDs []string) ([]*Comment, error)
	List(ctx context.Context, in ListCommentsInput) (*shared.PaginatedResult[*Comment], error)
	SetVisibility(ctx context.Context, accountID, commentID string, visibility CommentVisibility, at time.Time) error
	SetLiked(ctx context.Context, accountID, commentID string, liked bool) error
}

type PurgeRepository interface {
	ConversationIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error)
	DeleteConversations(ctx context.Context, accountID string, ids []string) error
	StoredObjectKeys(ctx context.Context, accountID string, limit int) ([]string, error)
	ForgetObjectKeys(ctx context.Context, accountID string, keys []string) error
	ContactIDsForPurge(ctx context.Context, accountID string, limit int) ([]string, error)
	DeleteContacts(ctx context.Context, accountID string, ids []string) error
	DeleteAccountRows(ctx context.Context, accountID string, limit int) (done bool, err error)
}
