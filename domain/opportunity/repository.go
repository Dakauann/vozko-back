package opportunity

import "vozko/domain/crmfilter"

type SearchByFilterInput struct {
	WorkspaceID string

	Filter crmfilter.Filter

	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string

	SortField string
	SortOrder string

	Page     int
	PageSize int
}

type Store interface {
	Create(o *Opportunity, links []ConversationLink, events []Event) error
	Update(o *Opportunity, events []Event) error
	Link(link ConversationLink, events []Event) error
	OpenForEntry(workspaceID, pipelineID, entryID, entryType string) (*Opportunity, error)
}

type Repository interface {
	Store

	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Opportunity, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*Opportunity, error)
	ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*Opportunity, error)

	SearchByFilter(input SearchByFilterInput) ([]*Opportunity, int64, error)

	SumValueByFilter(input SearchByFilterInput) (int64, error)

	ListEvents(workspaceID, opportunityID string) ([]Event, error)
	CurrentForEntry(workspaceID, pipelineID, entryID, entryType string) (*Opportunity, error)

	WithEntryLock(workspaceID, entryID, entryType string, fn func(Store) error) error
}

type ConversationLink struct {
	OpportunityID string `json:"opportunityId"`
	EntryID       string `json:"entryId"`
	EntryType     string `json:"entryType"`
}

type LinkRepository interface {
	Unlink(opportunityID, entryID, entryType string) error
	ListByOpportunity(workspaceID, opportunityID string) ([]ConversationLink, error)
	ListByEntry(workspaceID, entryID, entryType string) ([]ConversationLink, error)
}

type OwnerDirectory interface {
	Belongs(workspaceID, actorID string) (bool, error)
}
