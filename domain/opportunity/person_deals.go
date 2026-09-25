package opportunity

import (
	"errors"

	"vozko/domain/shared"
)

var (
	ErrEntryAccess = errors.New("opportunity: no access to this conversation")
	ErrScopeDenied = errors.New("opportunity: no access to this workspace's deals")
)

type DealDraft struct {
	PipelineID string
	StageID    string
	Title      string
	ValueCents int64
	Currency   string
	LeadID     string
	EntryID    string
	EntryType  string
}

type DealScope struct {
	DepartmentIDs    []string
	Restrict         bool
	AssigneeOverride string
}

type PersonDealsUseCase interface {
	Scope(by shared.Person, workspaceID string) (DealScope, error)
	Get(by shared.Person, workspaceID, dealID string) (*Opportunity, error)
	Create(by shared.Person, workspaceID string, draft DealDraft) (*Opportunity, error)
	Move(by shared.Person, workspaceID, dealID, stageID string) (*Opportunity, error)
	Link(by shared.Person, workspaceID, dealID, entryID, entryType string) error
	ListByPipeline(by shared.Person, workspaceID, pipelineID string) ([]*Opportunity, error)
}
