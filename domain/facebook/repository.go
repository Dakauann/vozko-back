package facebook

import (
	"context"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type GrantRepository interface {
	Upsert(ctx context.Context, g *Grant) error
	FindByID(ctx context.Context, id string) (*Grant, error)
	ListActive(ctx context.Context, limit int) ([]*Grant, error)
	ListByAppScopedUser(ctx context.Context, appScopedUserID string) ([]*Grant, error)
	MarkChecked(ctx context.Context, id string, scopes []string, granular map[string][]string, at time.Time) error
	Revoke(ctx context.Context, id string, at time.Time) error
	EraseToken(ctx context.Context, id string) error
	CountActivePages(ctx context.Context, grantID string) (int64, error)
}

type ListPagesInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Options     shared.QueryOptions
}

type PageRepository interface {
	Create(ctx context.Context, p *Page) error
	Update(ctx context.Context, p *Page) error
	UpdateConfig(ctx context.Context, p *Page) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateToken(ctx context.Context, id, grantID, pageToken string, scopes []string, tasks []Task) error
	UpdateSubscription(ctx context.Context, id string, fields []string, at time.Time) error
	UpdateRouting(ctx context.Context, id string, isDefault *bool, at time.Time) error
	UpdatePolicy(ctx context.Context, id, action, reason string, at time.Time) error
	UpdateProfile(ctx context.Context, id string, remote *RemotePage, pictureKey string, at time.Time) error

	FindByID(ctx context.Context, id string) (*Page, error)
	FindByFBPageID(ctx context.Context, fbPageID string) (*Page, error)
	FindByFBPageIDUnscoped(ctx context.Context, fbPageID string) (*Page, error)
	ListByWorkspace(ctx context.Context, in ListPagesInput) (*shared.PaginatedResult[*Page], error)
	ListByGrant(ctx context.Context, grantID string) ([]*Page, error)
	ListConnected(ctx context.Context, limit, offset int) ([]*Page, error)

	Restore(ctx context.Context, id string) error
	Delete(ctx context.Context, id string) error
}

type ContactRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, pageID, psid string) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByPSID(ctx context.Context, pageID, psid string) (*Contact, error)
	UpdateProfile(ctx context.Context, id string, p ContactProfile) error
	MarkUnreachable(ctx context.Context, id, reason string) error
	SetBlocked(ctx context.Context, id string, blocked bool) error
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, workspaceID, pageID, contactID string) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByContact(ctx context.Context, pageID, contactID string) (*Conversation, error)
	LatestForPage(ctx context.Context, pageID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	AdvanceWatermark(ctx context.Context, id string, kind WatermarkKind, at time.Time) error
	SetThreadOwner(ctx context.Context, id, appID string, at time.Time) error
	MergeMetadata(ctx context.Context, id string, values map[string]any) error
	SeedMetadata(ctx context.Context, id string, values map[string]any) error
	SetFBConversationID(ctx context.Context, id, fbConversationID string) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	CountByStatus(ctx context.Context, workspaceID, pageID string) (map[string]int64, error)
	StatusForEntry(ctx context.Context, id string) (string, error)
}

type PostRepository interface {
	UpsertMany(ctx context.Context, posts []*Post) error
	Track(ctx context.Context, post *Post) error
	FindByFBPostID(ctx context.Context, fbPostID string) (*Post, error)
	AppMadeAmong(ctx context.Context, fbPostIDs []string) (map[string]bool, error)
	SetHidden(ctx context.Context, fbPostID string, hidden bool) error
	UpdateMessage(ctx context.Context, fbPostID, message string) error
	Remove(ctx context.Context, fbPostID string) error
	AddCounts(ctx context.Context, fbPostID string, reactions, comments int) error
	ListByPage(ctx context.Context, pageID string, limit, offset int) ([]*Post, error)
}

type PublishJobRepository interface {
	Create(ctx context.Context, job *PublishJob) error
	FindByID(ctx context.Context, id string) (*PublishJob, error)
	Save(ctx context.Context, job *PublishJob) error
	Claim(ctx context.Context, id string, from, to JobStatus) (bool, error)
	ListByPage(ctx context.Context, pageID string, status JobStatus, limit int) ([]*PublishJob, error)
	ReleaseStale(ctx context.Context, before time.Time, limit int) ([]string, error)
	FindProcessingByVideoID(ctx context.Context, videoID string) (*PublishJob, error)
	ListDueProcessing(ctx context.Context, now time.Time, limit int) ([]*PublishJob, error)
	CountSince(ctx context.Context, pageID string, kind PublishKind, since time.Time) (int64, error)
}

type CommentRepository interface {
	UpsertMany(ctx context.Context, comments []*Comment) error
	FindByFBCommentID(ctx context.Context, fbCommentID string) (*Comment, error)
	SetHidden(ctx context.Context, fbCommentID string, hidden bool) error
	SetLiked(ctx context.Context, fbCommentID string, liked bool) error
	Edit(ctx context.Context, fbCommentID, message string, at time.Time) error
	MarkRemoved(ctx context.Context, fbCommentID string, at time.Time) error
	AddLikes(ctx context.Context, fbCommentID string, delta int) error
	LinkContact(ctx context.Context, fbCommentID, contactID string) error
	ContactsFor(ctx context.Context, fbCommentIDs []string) (map[string]string, error)
}
