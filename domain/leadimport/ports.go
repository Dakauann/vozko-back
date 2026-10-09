package leadimport

import (
	"context"
	"errors"
	"time"

	"vozko/domain/customfield"
	"vozko/domain/lead"
	"vozko/domain/unofficial_whatsapp"
)

const MaxContactHolders = 5000

var ErrTooManyHolders = errors.New("lead import: more leads hold these phones than one lookup may return")

type Guard struct {
	From  []Status
	Claim string
}

type Ref struct {
	ID          string
	WorkspaceID string
}

type Stalled struct {
	Ref
	Status Status
}

type IssueRow struct {
	Seq int64
	lead.ImportIssue
}

type Store interface {
	Create(ctx context.Context, j *Job) error
	Get(ctx context.Context, workspaceID, id string) (*Job, error)
	Save(ctx context.Context, j *Job, guard Guard) error
	Claim(ctx context.Context, id, token string, now time.Time) (*Job, error)
	Claimable(ctx context.Context, now time.Time, limit int) ([]Ref, error)
	FailStalled(ctx context.Context, now time.Time) ([]Stalled, error)
	Expired(ctx context.Context, now time.Time, limit int) ([]Job, error)
	Delete(ctx context.Context, id string) error
	Issues(ctx context.Context, importID string, after int64, limit int) ([]IssueRow, error)
	Unused(ctx context.Context, workspaceID, requestedBy string) ([]Job, error)
	Mine(ctx context.Context, workspaceID, requestedBy string, now time.Time, limit int) ([]Job, error)
	Placement(ctx context.Context, workspaceID, importID string) (Placement, error)
}

type RowItem struct {
	Record   lead.ImportRecord
	Existing *lead.Lead
}

type RowOutcome struct {
	Record   lead.ImportRecord
	Decision lead.ImportDecision
	Matched  *lead.Lead
	LeadID   string
}

type PendingLink struct {
	ID             string
	Line           int
	LeadID         string
	RelativeNumber string
	Kind           lead.RelationKind
}

type Checkpoint struct {
	Processed int
	Result    Counts
	Issues    []lead.ImportIssue
	Links     []PendingLink
}

type Decide func(existing *lead.Lead, row lead.ImportRecord) (lead.ImportDecision, error)

type RowBatch struct {
	Job         *Job
	ActorID     string
	Definitions []*customfield.Definition
	Items       []RowItem
	Decide      Decide
	Checkpoint  func(outcomes []RowOutcome) Checkpoint
}

type LinkWrite struct {
	Link     PendingLink
	Relation *lead.Relation
	Issue    *lead.ImportIssue
}

type LinkBatch struct {
	Job        *Job
	Links      []LinkWrite
	Checkpoint func(created int, failed []lead.ImportIssue) Counts
}

type Writer interface {
	WriteRows(ctx context.Context, batch RowBatch) error
	PendingLinks(ctx context.Context, importID string, limit int) ([]PendingLink, error)
	WriteLinks(ctx context.Context, batch LinkBatch) error
}

type Lookup interface {
	ByIdentity(ctx context.Context, workspaceID string, numbers []string) (map[string]*lead.Lead, error)
	PhoneHolderCounts(ctx context.Context, workspaceID string, numbers []string) (map[string]int, error)
	HoldingPhones(ctx context.Context, workspaceID string, numbers []string) ([]*lead.Lead, error)
	IdentityIDs(ctx context.Context, workspaceID string, numbers []string) (map[string]string, error)
}

type Members interface {
	UserIDsByEmail(ctx context.Context, workspaceID string, emails []string) (map[string]string, error)
}

type Files interface {
	Store(ctx context.Context, workspaceID, name string, data []byte) (string, error)
	Read(ctx context.Context, workspaceID, mediaID string) ([]byte, error)
	ReadLibrary(ctx context.Context, workspaceID, mediaID string) ([]byte, error)
	Erase(ctx context.Context, workspaceID, mediaID string) error
}

type Seeder interface {
	Publish(in unofficial_whatsapp.SeedRequest) (unofficial_whatsapp.SeedQueued, error)
}
