package smartcampaignhttp

import (
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	sc "vozko/domain/whatsapp_campaign/smartcampaign"
)

func WriteRefusal(w http.ResponseWriter, err error) bool {
	var blocked *sc.BlockedError
	switch {
	case errors.As(err, &blocked):
		rules := make([]string, 0, len(blocked.Decision.Findings))
		for _, f := range blocked.Decision.Drivers() {
			rules = append(rules, string(f.Rule))
		}
		response.WriteErrorWithCode(w, http.StatusConflict, "smart_campaign_blocked", sc.ErrStillUnhealthy.Error(), map[string]string{"reasons": strings.Join(rules, ",")})
	case errors.Is(err, sc.ErrInvalidPolicy):
		response.WriteErrorWithCode(w, http.StatusBadRequest, "smart_campaign_invalid_policy", err.Error(), nil)
	case errors.Is(err, sc.ErrCampaignBusy):
		response.WriteErrorWithCode(w, http.StatusConflict, "smart_campaign_busy", err.Error(), nil)
	default:
		return false
	}
	return true
}
