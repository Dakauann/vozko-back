package campaignaudience_usecase

import (
	"context"
	"time"

	"vozko/domain/campaign"
	"vozko/domain/campaignaudience"
	"vozko/domain/conversation"
	"vozko/domain/customfield"
	"vozko/domain/emaildesign"
	"vozko/domain/lead"
	"vozko/domain/selection"
	"vozko/domain/sheet"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
)

type Actor = conversation.Viewer

func scopeOf(a Actor) selection.Scope {
	return selection.Scope{WorkspaceID: a.WorkspaceID, ActorID: a.UserID, IsAdmin: a.IsAdmin}
}

type Params struct {
	TemplateID      string
	BusinessPhoneID string
	InstanceID      string
	Message         *uwc.MessageSpec
	Subject         string
	Document        *emaildesign.Document
	Kind            string
	Automation      campaign.Automation
}

type Prepared struct {
	Slots    []campaignaudience.Slot
	Awaits   campaignaudience.Blocker
	Metadata campaignaudience.MetadataRule
	Open     func(ctx context.Context, bound []campaignaudience.Bound, source campaignaudience.Source) (Judge, error)
}

type RowCandidate struct {
	Row      campaignaudience.Row
	LeadID   string
	Values   []string
	Metadata map[string]any
}

type Judge interface {
	Leads(ctx context.Context, ids []string) ([]campaignaudience.Candidate, error)
	Rows(ctx context.Context, rows []RowCandidate) ([]campaignaudience.Candidate, error)
}

type Channel interface {
	Name() campaignaudience.Channel
	Prepare(ctx context.Context, a Actor, params Params) (Prepared, error)
}

type Selections interface {
	Resolve(ctx context.Context, scope selection.Scope, s selection.Selection, after string, limit int) ([]selection.Ref, error)
	Selected(ctx context.Context, scope selection.Scope, s selection.Selection) (int, error)
}

type Lists interface {
	Create(ctx context.Context, list campaignaudience.List, rows []campaignaudience.Row) error
	Get(ctx context.Context, workspaceID, id string) (*campaignaudience.List, error)
	Save(ctx context.Context, list campaignaudience.List) error
	Delete(ctx context.Context, workspaceID, id string) error
	RowsAfter(ctx context.Context, listID string, after campaignaudience.RowCursor, limit int) ([]campaignaudience.Row, error)
	SearchRows(ctx context.Context, listID string, q campaignaudience.RowQuery) ([]campaignaudience.Row, int, error)
	CountRows(ctx context.Context, listID string) (int, error)
	Row(ctx context.Context, listID, rowID string) (campaignaudience.Row, error)
	AddRow(ctx context.Context, row campaignaudience.Row) error
	UpdateRow(ctx context.Context, row campaignaudience.Row) error
	DeleteRow(ctx context.Context, listID, rowID string) error
	DeleteExpired(ctx context.Context, now time.Time, limit int) (int, error)
}

type Sheets interface {
	Read(data []byte) ([]sheet.Row, error)
}

type Phones interface {
	IdentityIDs(ctx context.Context, workspaceID string, numbers []string) (map[string]string, error)
}

type Emails interface {
	IDsByEmails(ctx context.Context, workspaceID string, addresses []string) (map[string]string, error)
}

type EmailLeads interface {
	FindOrCreateManyByEmail(ctx context.Context, workspaceID string, inputs []lead.BulkEmailInput) (map[string]*lead.Lead, error)
}

type Definitions interface {
	ListByObject(workspaceID string, objectType customfield.ObjectType) ([]*customfield.Definition, error)
}

type Suggestions interface {
	Get(ctx context.Context, workspaceID, templateID string) ([]campaignaudience.Suggestion, error)
	Replace(ctx context.Context, workspaceID, templateID, actorID string, suggestions []campaignaudience.Suggestion) error
}

type Recipient struct {
	LeadID    string
	Number    string
	Name      string
	Variables []string
	Metadata  map[string]any
	Skip      campaign.SkipReason
	Missing   []campaign.MissingVariable
}
