package webchat

import (
	"context"
	"time"

	"vozko/domain/conversation"
	"vozko/domain/shared"
)

type WidgetRepository interface {
	Create(ctx context.Context, w *Widget) error
	Update(ctx context.Context, w *Widget) error
	FindByID(ctx context.Context, workspaceID, id string) (*Widget, error)
	FindByIDUnscoped(ctx context.Context, id string) (*Widget, error)
	FindByPublicKey(ctx context.Context, publicKey string) (*Widget, error)
	ListByWorkspace(ctx context.Context, in ListWidgetsInput) (*shared.PaginatedResult[*Widget], error)
	Delete(ctx context.Context, workspaceID, id string) error
}

type ListWidgetsInput struct {
	WorkspaceID string
	Search      string
	Options     shared.QueryOptions
}

type VisitorRepository interface {
	Create(ctx context.Context, v *Visitor) error
	FindByID(ctx context.Context, id string) (*Visitor, error)
	FindByIDs(ctx context.Context, ids []string) ([]*Visitor, error)
	FindByExternalID(ctx context.Context, widgetID, externalID string) (*Visitor, error)
	ApplyIdentity(ctx context.Context, id string, claims IdentityClaims) error
	SaveIntake(ctx context.Context, id string, intake Intake, leadID *string, at time.Time) error
	Touch(ctx context.Context, id string, seen VisitorSighting) error
	SetBlocked(ctx context.Context, id string, blocked bool, at time.Time) error
}

type VisitorSighting struct {
	At         time.Time
	IPHash     string
	UserAgent  string
	Locale     string
	PageOrigin string
}

type ConversationRepository interface {
	FindOrCreate(ctx context.Context, in FindOrCreateConversationInput) (*Conversation, error)
	FindByID(ctx context.Context, id string) (*Conversation, error)
	FindByVisitor(ctx context.Context, widgetID, visitorID string) (*Conversation, error)
	WorkspaceIDForEntry(ctx context.Context, entryID string) (string, error)
	DepartmentIDForEntry(ctx context.Context, entryID string) (string, error)
	ListEntryIDsByWorkspace(ctx context.Context, workspaceID string) ([]string, error)
	RecordInbound(ctx context.Context, id string, at time.Time) error
	RecordOutbound(ctx context.Context, id string, at time.Time) error
	SetStatus(ctx context.Context, id string, write conversation.StatusWrite) error
	SetAutomationEnabled(ctx context.Context, id string, enabled *bool) error
	StatusForEntry(ctx context.Context, id string) (string, error)
	CountByStatus(ctx context.Context, workspaceID, widgetID string) (map[string]int64, error)
	SetPendingOptions(ctx context.Context, id string, options []Option) error
}
