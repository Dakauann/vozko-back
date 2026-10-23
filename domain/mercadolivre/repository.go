package mercadolivre

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
}

type RefreshFunc func(ctx context.Context, account *Account) (RefreshOutcome, error)

type ListAccountsInput struct {
	WorkspaceID string
	Search      string
	Status      *Status
	Options     shared.QueryOptions
}

type AccountRepository interface {
	Create(ctx context.Context, a *Account) error
	Update(ctx context.Context, a *Account) error
	UpdateStatus(ctx context.Context, id string, status Status, reason string) error
	UpdateCapabilities(ctx context.Context, id string, caps AccountCapabilities) error
	UpdateNames(ctx context.Context, id, nickname, displayName string) error

	FindByID(ctx context.Context, id string) (*Account, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Account, error)
	FindByIDInWorkspace(ctx context.Context, workspaceID, id string) (*Account, error)
	FindLiveByMLUserID(ctx context.Context, mlUserID string) (*Account, error)
	FindByWorkspaceAndMLUserIDUnscoped(ctx context.Context, workspaceID, mlUserID string) (*Account, error)
	Restore(ctx context.Context, id string) error
	Reconnect(ctx context.Context, a *Account) error

	ListByWorkspace(ctx context.Context, in ListAccountsInput) (*shared.PaginatedResult[*Account], error)

	ListDueForRefresh(ctx context.Context, expiresBefore time.Time, limit int) ([]*Account, error)
	LockForRefresh(ctx context.Context, id string, refresh RefreshFunc) (acquired bool, err error)

	ListDueForReconcile(ctx context.Context, reconciledBefore time.Time, limit int) ([]*Account, error)
	MarkReconciled(ctx context.Context, id string, at time.Time) error

	ListDueForHealthCheck(ctx context.Context, checkedBefore time.Time, limit int) ([]*Account, error)
	MarkHealthChecked(ctx context.Context, id string, at time.Time) error

	Disconnect(ctx context.Context, id string, status Status, reason string, purgeAfter time.Time) error
	ListDueForPurge(ctx context.Context, now time.Time, limit int) ([]*Account, error)
	DeletePurged(ctx context.Context, id string) error
}

type FindOrCreateContactInput struct {
	WorkspaceID string
	AccountID   string
	MLUserID    string
	Nickname    string
}

type ContactRepository interface {
	FindOrCreateByUserID(ctx context.Context, in FindOrCreateContactInput) (*Contact, error)
	FindByID(ctx context.Context, id string) (*Contact, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Contact, error)
	FindByUserID(ctx context.Context, accountID, mlUserID string) (*Contact, error)
	UpdateNickname(ctx context.Context, id, nickname string, at time.Time) error
	SetLeadID(ctx context.Context, id string, leadID *string) error
	SetQuestionsBlocked(ctx context.Context, id string, at *time.Time) error
	MarkQuestionsBlocked(ctx context.Context, accountID string, mlUserIDs []string, at, unchangedSince time.Time) error
	ClearQuestionsBlockedExcept(ctx context.Context, accountID string, keepMLUserIDs []string, blockedBefore time.Time) error
}

type FindOrCreateConversationInput struct {
	WorkspaceID   string
	AccountID     string
	ContactID     string
	PackKey       string
	OrderIDs      []string
	BuyerMLUserID string
}

type ConversationRepository interface {
	FindOrCreateByPack(ctx context.Context, in FindOrCreateConversationInput) (conv *Conversation, created bool, err error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Conversation, error)
	FindByPack(ctx context.Context, accountID, packKey string) (*Conversation, error)
	FindByShipment(ctx context.Context, accountID, shipmentID string) (*Conversation, error)

	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)

	ApplyStatus(ctx context.Context, id string, state ThreadState) error
	ClaimOutbound(ctx context.Context, id string) (claimed bool, err error)
	ReleaseOutbound(ctx context.Context, id string) error

	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)

	UpdatePackSummary(ctx context.Context, id string, summary PackSummary, at time.Time) error
	SetBuyer(ctx context.Context, id, buyerMLUserID string) error

	CountByStatus(ctx context.Context, workspaceID, accountID string) (map[string]int64, error)
}

type ListListingsInput struct {
	AccountID string
	Search    string
	Status    string
	After     *shared.Keyset
	Limit     int
}

type ListingRepository interface {
	UpsertMany(ctx context.Context, listings []*Listing) error
	SetThumbnail(ctx context.Context, accountID, itemID, key, source string) error
	FindByItemID(ctx context.Context, accountID, itemID string) (*Listing, error)
	FindByItemIDs(ctx context.Context, accountID string, itemIDs []string) ([]*Listing, error)
	ListByAccount(ctx context.Context, in ListListingsInput) (*ListingList, error)
	ClaimSync(ctx context.Context, accountID string, now, syncedBefore time.Time) (claimed bool, err error)
	ReleaseSync(ctx context.Context, accountID string) error
}

type ListQuestionsInput struct {
	AccountID string
	ItemID    string
	Status    *QuestionStatus
	After     *QuestionCursor
	Limit     int
}

type AnswerRecord struct {
	Text             string
	Status           AnswerStatus
	AnsweredBy       AnswerSource
	AnsweredByUserID *string
	RuleID           *string
	AnsweredAt       time.Time
}

type QuestionRepository interface {
	Upsert(ctx context.Context, q *Question) (created bool, err error)
	FindByQuestionID(ctx context.Context, accountID, questionID string) (*Question, error)
	FindByQuestionIDs(ctx context.Context, accountID string, questionIDs []string) ([]*Question, error)
	Forget(ctx context.Context, accountID, questionID string) error
	List(ctx context.Context, in ListQuestionsInput) (*QuestionList, error)
	RecordAnswer(ctx context.Context, accountID, questionID string, answer AnswerRecord) error
	SetStatus(ctx context.Context, accountID, questionID string, status QuestionStatus) error
}

type PurgeRepository interface {
	ConversationIDs(ctx context.Context, accountID string, limit int) ([]string, error)
	PackThumbnailKeys(ctx context.Context, accountID string, conversationIDs []string) ([]string, error)
	DeleteConversations(ctx context.Context, accountID string, ids []string) error
	DeleteQuestions(ctx context.Context, accountID string, limit int) (deleted int64, err error)
	ListingThumbnails(ctx context.Context, accountID string, limit int) ([]ListingThumbnail, error)
	DeleteListings(ctx context.Context, accountID string, itemIDs []string) error
	DeleteListingSync(ctx context.Context, accountID string) error
	ContactIDs(ctx context.Context, accountID string, limit int) ([]string, error)
	DeleteContacts(ctx context.Context, accountID string, ids []string) error
}
