package httpx

import (
	"strings"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

type LeadSelectionRequest struct {
	Mode          string            `json:"mode" example:"all_matching"`
	IDs           []string          `json:"ids,omitempty"`
	Filter        *crmfilter.Filter `json:"filter,omitempty"`
	Sort          []crmfilter.Sort  `json:"sort,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	ExcludeIDs    []string          `json:"excludeIds,omitempty"`
	IncludeIDs    []string          `json:"includeIds,omitempty"`
	ExpectedCount int               `json:"expectedCount,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
}

func (r LeadSelectionRequest) Selection() selection.Selection {
	return selection.Selection{
		Mode: selection.Mode(strings.TrimSpace(r.Mode)), IDs: r.IDs, Filter: r.Filter,
		Sort: r.Sort, Limit: r.Limit, ExcludeIDs: r.ExcludeIDs, IncludeIDs: r.IncludeIDs,
		ExpectedCount: r.ExpectedCount, Fingerprint: r.Fingerprint,
	}
}

func LeadSelectionRequestOf(s selection.Selection) LeadSelectionRequest {
	return LeadSelectionRequest{
		Mode: string(s.Mode), IDs: s.IDs, Filter: s.Filter, Sort: s.Sort, Limit: s.Limit,
		ExcludeIDs: s.ExcludeIDs, IncludeIDs: s.IncludeIDs, ExpectedCount: s.ExpectedCount, Fingerprint: s.Fingerprint,
	}
}
