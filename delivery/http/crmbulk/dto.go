package crmbulk

import (
	"vozko/domain/crmfilter"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type BulkTargetRequest struct {
	EntryID   string `json:"entryId" example:"entry_a1b2c3"`
	EntryType string `json:"entryType" example:"conversation"`
}

// BulkApplyRequest names the action, the value, and WHICH entries to apply it to
// — either as an explicit target list or as the filter the operator's view was
// showing. Sending `filter` is how "select all 340 matching" works without the
// client having to page through and enumerate every id.
type BulkApplyRequest struct {
	Action  string              `json:"action" example:"move_stage"`
	Targets []BulkTargetRequest `json:"targets"`
	Value   string              `json:"value" example:"stage_a1b2c3"`
	// MoveToFunnel authorises a move_stage onto a stage of a DIFFERENT funnel,
	// for every target in this request. Without it the server refuses one, which
	// is what stops a mis-scoped selection from reorganising a whole board.
	MoveToFunnel bool `json:"moveToFunnel,omitempty"`
	// Filter is the same crmfilter.Filter shape GET /crm/entries takes, so the
	// client sends back verbatim the filter it rendered the table with. Used only
	// when targets is empty; the server re-runs it under the caller's own scope
	// rather than trusting a client-supplied id list.
	Filter *crmfilter.Filter `json:"filter,omitempty"`
}

type BulkFailureResponse struct {
	EntryID string `json:"entryId" example:"entry_a1b2c3"`
	Error   string `json:"error" example:"stage not found"`
}

type BulkResultResponse struct {
	Succeeded int                   `json:"succeeded" example:"3"`
	Failed    []BulkFailureResponse `json:"failed"`
	Forbidden bool                  `json:"forbidden,omitempty" example:"false"`
	// Matched and Truncated are set only for a filter-addressed bulk: how many
	// entries the filter selected, and whether the server's per-request cap stopped
	// short of all of them. Present so the UI can report "2000 de 5300" instead of
	// a bare count the operator cannot interpret.
	Matched   int64 `json:"matched,omitempty" example:"340"`
	Truncated bool  `json:"truncated,omitempty" example:"false"`
}

func toBulkResultResponse(r crmbulk_usecase.BulkResult) BulkResultResponse {
	var failed []BulkFailureResponse
	if r.Failed != nil {
		failed = make([]BulkFailureResponse, len(r.Failed))
		for i, f := range r.Failed {
			failed[i] = BulkFailureResponse{EntryID: f.EntryID, Error: f.Error}
		}
	}
	return BulkResultResponse{
		Succeeded: r.Succeeded,
		Failed:    failed,
		Forbidden: r.Forbidden,
		Matched:   r.Matched,
		Truncated: r.Truncated,
	}
}
