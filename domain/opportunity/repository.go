package opportunity

import "vozko/domain/crmfilter"

// SearchByFilterInput is the workspace-scoped, filter-driven read path that backs
// the opportunity board (the "Funil de Vendas") and its flat list. Unlike
// ListByPipeline (which hard-codes the pipeline predicate) it carries a reusable
// crmfilter.Filter compiled to SQL by the infra crmfilter OpportunityDescriptor,
// so the board, the per-column queries and the flat list all share ONE predicate
// surface. It is ADDITIVE: the lifecycle methods below are untouched.
//
// MONEY GUARDRAIL: SortField "value" and any value predicate operate on
// value_cents (the customer's own BRL sales figure). This is NOT Vozko platform
// money and never touches the USD-micros wallet / saldo / ledger / FX.
type SearchByFilterInput struct {
	WorkspaceID string

	Filter crmfilter.Filter

	// Department scope (mirrors conversation.SearchByFilterInput). When
	// RestrictDepartments is set, only deals whose OWNER belongs to one of
	// DepartmentIDs — OR that the requesting user owns (AssigneeOverrideUserID) — are
	// returned. Empty/false = workspace-wide (admins/owners). This closes the
	// cross-department visibility gap on the deal board.
	DepartmentIDs          []string
	RestrictDepartments    bool
	AssigneeOverrideUserID string

	// SortField: "value" (value_cents), "close_date", or "created"/"created_at"
	// (default). SortOrder: "asc" | "desc" (default desc).
	SortField string
	SortOrder string

	Page     int
	PageSize int
}

// Repository is the persistence port for opportunities. Filtered board/list
// reads go through the shared crmfilter compiler (an opportunity ObjectDescriptor)
// rather than bespoke methods here, so this port stays focused on lifecycle plus
// the two filter-driven reads the board needs.
type Repository interface {
	Create(o *Opportunity) error
	Update(o *Opportunity) error
	Delete(workspaceID, id string) error
	GetByID(workspaceID, id string) (*Opportunity, error)
	ListByPipeline(workspaceID, pipelineID string) ([]*Opportunity, error)
	// ListByPipelineScoped is ListByPipeline with the owner-department scope applied,
	// for user-facing whole-pipeline reads (flat list + CSV export). restrict=false =
	// workspace-wide (admins/owners).
	ListByPipelineScoped(workspaceID, pipelineID string, departmentIDs []string, restrict bool, assigneeOverrideUserID string) ([]*Opportunity, error)

	// SearchByFilter returns a page of opportunities matching the compiled filter
	// (workspace-scoped) plus the TOTAL count across all matches (for the column
	// header / list pagination).
	SearchByFilter(input SearchByFilterInput) ([]*Opportunity, int64, error)

	// SumValueByFilter returns the summed value_cents across ALL rows matching the
	// compiled filter (not just the current page), for a board column's monetary
	// total. Returns 0 when nothing matches. MONEY GUARDRAIL: value_cents only.
	SumValueByFilter(input SearchByFilterInput) (int64, error)
}

// ConversationLink attaches a conversation (entry) to an opportunity so the deal
// timeline can aggregate its WhatsApp/voice history.
type ConversationLink struct {
	OpportunityID string `json:"opportunityId"`
	EntryID       string `json:"entryId"`
	EntryType     string `json:"entryType"` // voice | whatsapp | support
}

// LinkRepository is the persistence port for opportunity <-> conversation links.
type LinkRepository interface {
	Link(link ConversationLink) error
	Unlink(opportunityID, entryID, entryType string) error
	ListByOpportunity(workspaceID, opportunityID string) ([]ConversationLink, error)
	ListByEntry(workspaceID, entryID, entryType string) ([]ConversationLink, error)
}
