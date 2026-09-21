package whatsapp_campaign_entry

import "time"

type WorkspaceSummaryFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	Type          string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

type SummaryAggregator interface {
	CountByStatusForWorkspace(filter WorkspaceSummaryFilter) (*StatusCounts, error)
	CountDispatchesByCategoryForWorkspace(filter WorkspaceSummaryFilter) (map[string]int64, error)
}
