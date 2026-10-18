package copilottools

import (
	"errors"

	"vozko/domain/copilot"
	"vozko/domain/workspace"
)

func RequesterOf(cc copilot.Context) workspace.Requester {
	return workspace.Requester{UserID: cc.UserID, SystemAdmin: cc.SystemAdmin}
}

func ConsentGrantRefusal(err error) (copilot.Result, bool) {
	if !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		return copilot.Result{}, false
	}
	return copilot.Result{Status: copilot.StatusDenied, Message: "nada foi salvo (" + workspace.CodeConsentGrantForbidden +
		"): registrar consentimento de leads exige a permissão leads:consent, que o usuário não tem"}, true
}
