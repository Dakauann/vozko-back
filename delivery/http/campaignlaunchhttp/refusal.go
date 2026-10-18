package campaignlaunchhttp

import (
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/campaign/launchcheck"
)

func WriteRefusal(w http.ResponseWriter, err error) bool {
	var refused *launchcheck.RefusedError
	if !errors.As(err, &refused) {
		return false
	}
	code := "campaign_launch_unconfirmed"
	if refused.Report.Blocks() {
		code = "campaign_launch_blocked"
	}
	response.WriteErrorWithCode(w, http.StatusConflict, code, err.Error(), map[string]string{
		"blocks":      codesAt(refused.Report, launchcheck.Block),
		"confirms":    codesAt(refused.Report, launchcheck.Confirm),
		"fingerprint": refused.Report.Fingerprint(),
	})
	return true
}

func codesAt(r launchcheck.Report, level launchcheck.Level) string {
	var codes []string
	for _, f := range r.Findings {
		if f.Level == level {
			codes = append(codes, string(f.Code))
		}
	}
	return strings.Join(codes, ",")
}
