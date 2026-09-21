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

type Repository interface {
	Create(o *Opportunity) error
	Update(o *Opportunity) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Opportunity, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*Opportunity, error)
	ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*Opportunity, error)

	SearchByFilter(input SearchByFilterInput) ([]*Opportunity, int64, error)

	SumValueByFilter(input SearchByFilterInput) (int64, error)
}

type ConversationLink struct {
	OpportunityID string `json:"opportunityId"`
	EntryID       string `json:"entryId"`
	EntryType     string `json:"entryType"`
}

type LinkRepository interface {
	Link(link ConversationLink) error
	Unlink(opportunityID, entryID, entryType string) error
	ListByOpportunity(workspaceID, opportunityID string) ([]ConversationLink, error)
	ListByEntry(workspaceID, entryID, entryType string) ([]ConversationLink, error)
}
