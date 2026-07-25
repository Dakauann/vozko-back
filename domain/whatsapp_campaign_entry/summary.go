package whatsapp_campaign_entry

import "time"

// WorkspaceSummaryFilter selects which campaigns' entries feed a workspace-level
// metrics rollup (the "disparos" summary on the campaigns list). The date range
// filters campaigns by their CREATION date: CreatedFrom/CreatedTo are inclusive
// bounds and a nil bound means unbounded, so an empty filter yields the all-time
// total. Type narrows to a WhatsApp campaign type ("standard"/"organic"); empty
// means every type. DepartmentIDs, when set, restrict to those departments.
type WorkspaceSummaryFilter struct {
	WorkspaceID   string
	DepartmentIDs []string
	Type          string
	CreatedFrom   *time.Time
	CreatedTo     *time.Time
}

// SummaryAggregator rolls entry status counts up to the workspace level for the
// campaigns matching a filter, in a single query. It is intentionally separate
// from Repository so the summary feature doesn't force every existing Repository
// mock to grow a method; the concrete repository implements both.
type SummaryAggregator interface {
	CountByStatusForWorkspace(filter WorkspaceSummaryFilter) (*StatusCounts, error)
	// CountDispatchesByCategoryForWorkspace returns billed-send volume
	// (same definition as StatusCounts.Dispatches) grouped by the linked
	// WhatsApp template category (MARKETING / UTILITY / AUTHENTICATION).
	// Keys are uppercased categories; unknown/missing template is omitted.
	CountDispatchesByCategoryForWorkspace(filter WorkspaceSummaryFilter) (map[string]int64, error)
}
