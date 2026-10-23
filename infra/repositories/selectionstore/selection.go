package selectionstore

import (
	"encoding/json"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
)

type stored struct {
	Mode          selection.Mode    `json:"mode"`
	IDs           []string          `json:"ids,omitempty"`
	Filter        *crmfilter.Filter `json:"filter,omitempty"`
	Sort          []crmfilter.Sort  `json:"sort,omitempty"`
	Limit         int               `json:"limit,omitempty"`
	ExcludeIDs    []string          `json:"exclude_ids,omitempty"`
	IncludeIDs    []string          `json:"include_ids,omitempty"`
	ExpectedCount int               `json:"expected_count,omitempty"`
	Fingerprint   string            `json:"fingerprint,omitempty"`
	Require       *crmfilter.Filter `json:"require,omitempty"`
}

func Encode(s selection.Selection) ([]byte, error) {
	return json.Marshal(stored{
		Mode: s.Mode, IDs: s.IDs, Filter: s.Filter, Sort: s.Sort, Limit: s.Limit, ExcludeIDs: s.ExcludeIDs, IncludeIDs: s.IncludeIDs,
		ExpectedCount: s.ExpectedCount, Fingerprint: s.Fingerprint, Require: s.Require,
	})
}

func Decode(raw []byte) (selection.Selection, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return selection.Selection{}, nil
	}
	var s stored
	if err := json.Unmarshal(raw, &s); err != nil {
		return selection.Selection{}, err
	}
	return selection.Selection{
		Mode: s.Mode, IDs: s.IDs, Filter: s.Filter, Sort: s.Sort, Limit: s.Limit, ExcludeIDs: s.ExcludeIDs, IncludeIDs: s.IncludeIDs,
		ExpectedCount: s.ExpectedCount, Fingerprint: s.Fingerprint, Require: s.Require,
	}, nil
}
