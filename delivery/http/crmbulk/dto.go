package crmbulk

import (
	"vozko/domain/crmfilter"
	crmbulk_usecase "vozko/usecases/crmbulk"
)

type BulkTargetRequest struct {
	EntryID   string `json:"entryId" example:"entry_a1b2c3"`
	EntryType string `json:"entryType" example:"conversation"`
}

type BulkApplyRequest struct {
	Action  string              `json:"action" example:"move_stage"`
	Targets []BulkTargetRequest `json:"targets"`
	Value   string              `json:"value" example:"stage_a1b2c3"`
	Filter  *crmfilter.Filter   `json:"filter,omitempty"`
}

type BulkFailureResponse struct {
	EntryID string `json:"entryId" example:"entry_a1b2c3"`
	Error   string `json:"error" example:"stage not found"`
}

type BulkResultResponse struct {
	Succeeded int                   `json:"succeeded" example:"3"`
	Failed    []BulkFailureResponse `json:"failed"`
	Forbidden bool                  `json:"forbidden,omitempty" example:"false"`
	Matched   int64                 `json:"matched,omitempty" example:"340"`
	Truncated bool                  `json:"truncated,omitempty" example:"false"`
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
