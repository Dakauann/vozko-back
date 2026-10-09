package crmbulk

import (
	"strings"

	"vozko/domain/crmfilter"
	"vozko/domain/selection"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type BulkTargetRequest struct {
	EntryID   string `json:"entryId" example:"entry_a1b2c3"`
	EntryType string `json:"entryType" example:"conversation"`
}

type BulkApplyRequest struct {
	Action        string              `json:"action" example:"move_stage"`
	Targets       []BulkTargetRequest `json:"targets"`
	Value         string              `json:"value" example:"stage_a1b2c3"`
	Filter        *crmfilter.Filter   `json:"filter,omitempty"`
	Mode          string              `json:"mode,omitempty" enums:"ids,all_matching,everyone" example:"all_matching"`
	ExpectedCount int                 `json:"expectedCount,omitempty" example:"340"`
	Fingerprint   string              `json:"fingerprint,omitempty" example:"9f2c4e"`
	ExcludeIDs    []string            `json:"excludeIds,omitempty"`
}

type BulkFailureResponse struct {
	EntryID string `json:"entryId" example:"entry_a1b2c3"`
	Error   string `json:"error" example:"stage not found"`
}

type BulkResultResponse struct {
	Succeeded int                   `json:"succeeded" example:"3"`
	Failed    []BulkFailureResponse `json:"failed"`
	Matched   int                   `json:"matched,omitempty" example:"340"`
	Eligible  int                   `json:"eligible,omitempty" example:"340"`
	Truncated bool                  `json:"truncated,omitempty" example:"false"`
}

type SelectionChangedResponse struct {
	Error    bool   `json:"error" example:"true"`
	Code     string `json:"code" example:"selection_changed"`
	Message  string `json:"message"`
	Expected int    `json:"expected" example:"340"`
	Matched  int    `json:"matched" example:"352"`
}

type BulkCountRequest struct {
	Filter *crmfilter.Filter `json:"filter"`
}

type BulkCountResponse struct {
	Matched     int    `json:"matched" example:"340"`
	Fingerprint string `json:"fingerprint" example:"9f2c4e"`
}

func (req BulkApplyRequest) selection() selection.Selection {
	return selection.Selection{
		Mode:          selection.Mode(req.Mode),
		Filter:        req.Filter,
		ExcludeIDs:    req.ExcludeIDs,
		ExpectedCount: req.ExpectedCount,
		Fingerprint:   req.Fingerprint,
	}
}

func (req BulkApplyRequest) targets() []crmbulk_usecase.EntryRef {
	out := make([]crmbulk_usecase.EntryRef, 0, len(req.Targets))
	for _, t := range req.Targets {
		out = append(out, crmbulk_usecase.EntryRef{EntryID: strings.TrimSpace(t.EntryID), EntryType: strings.TrimSpace(t.EntryType)})
	}
	return out
}

func toBulkResultResponse(r crmbulk_usecase.BulkResult) BulkResultResponse {
	failed := make([]BulkFailureResponse, len(r.Failed))
	for i, f := range r.Failed {
		failed[i] = BulkFailureResponse{EntryID: f.ID, Error: f.Error}
	}
	return BulkResultResponse{
		Succeeded: r.Succeeded,
		Failed:    failed,
		Matched:   r.Matched,
		Eligible:  r.Eligible,
		Truncated: r.Truncated,
	}
}
