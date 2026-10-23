package buttonactionhttp

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/buttonaction"
	"vozko/domain/workflow"
)

var refusalStatuses = []struct {
	err    error
	status int
}{
	{buttonaction.ErrWorkspaceRequired, http.StatusForbidden},
	{buttonaction.ErrActorRequired, http.StatusUnauthorized},
	{buttonaction.ErrForbidden, http.StatusForbidden},
	{buttonaction.ErrTemplateNotFound, http.StatusNotFound},
	{buttonaction.ErrCampaignNotFound, http.StatusNotFound},
	{buttonaction.ErrCampaignNoTemplate, http.StatusUnprocessableEntity},
	{buttonaction.ErrParamsNotInWorkspace, http.StatusUnprocessableEntity},
	{buttonaction.ErrExitLocked, http.StatusUnprocessableEntity},
	{buttonaction.ErrButtonActionStale, http.StatusUnprocessableEntity},
	{buttonaction.ErrExitRiskNotAcknowledged, http.StatusUnprocessableEntity},
	{buttonaction.ErrMarketingNeedsExit, http.StatusUnprocessableEntity},
	{workflow.ErrWorkflowNeedsMessageTrigger, http.StatusUnprocessableEntity},
	{buttonaction.ErrUnavailable, http.StatusServiceUnavailable},
}

func Refusal(err error) (int, string, bool) {
	code := buttonaction.ErrorCode(err)
	if code == "" {
		return 0, "", false
	}
	for _, known := range refusalStatuses {
		if errors.Is(err, known.err) {
			return known.status, code, true
		}
	}
	return http.StatusBadRequest, code, true
}

func WriteRefusal(w http.ResponseWriter, err error) bool {
	status, code, ok := Refusal(err)
	if !ok {
		return false
	}
	response.WriteErrorWithCode(w, status, code, err.Error(), nil)
	return true
}
