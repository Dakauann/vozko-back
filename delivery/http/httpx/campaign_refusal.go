package httpx

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/usecases/campaignguard"
)

func WriteSelectionSendRefusal(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, campaign.ErrSelectionSendLocked), errors.Is(err, campaign.ErrSelectionStartNeedsReview):
		response.WriteErrorWithCode(w, http.StatusConflict, campaign.ErrorCode(err), err.Error(), nil)
	case errors.Is(err, campaignguard.ErrUnavailable):
		response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "campaign_guard_unavailable", err.Error(), nil)
	default:
		return false
	}
	return true
}
