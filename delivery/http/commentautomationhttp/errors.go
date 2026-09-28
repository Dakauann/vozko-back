package commentautomationhttp

import (
	"errors"
	"net/http"

	"vozko/delivery/http/response"
	ca "vozko/domain/commentautomation"
	"vozko/domain/privatereply"
)

var invalidRule = []error{
	ca.ErrNoActions, ca.ErrNameRequired, ca.ErrEmptyKeywords, ca.ErrReplyEmpty,
	ca.ErrUnknownAction, ca.ErrActionUnsupported, ca.ErrUnknownSource,
}

func WriteError(w http.ResponseWriter, err error) bool {
	switch {
	case errors.Is(err, privatereply.ErrUsed):
		response.WriteErrorWithCode(w, http.StatusConflict, "private_reply_used", "Only one private reply is allowed per comment", nil)
	case errors.Is(err, privatereply.ErrExpired):
		response.WriteErrorWithCode(w, http.StatusConflict, "private_reply_expired", "Private replies must be sent within 7 days of the comment", nil)
	case errors.Is(err, privatereply.ErrDeadlineUnknown):
		response.WriteErrorWithCode(w, http.StatusConflict, "private_reply_deadline_unknown", "The comment date could not be confirmed, so no private reply was sent", nil)
	case errors.Is(err, ca.ErrRuleNotFound):
		response.WriteErrorWithCode(w, http.StatusNotFound, "not_found", "Comment rule not found", nil)
	default:
		for _, target := range invalidRule {
			if errors.Is(err, target) {
				response.WriteErrorWithCode(w, http.StatusBadRequest, "invalid_rule", err.Error(), nil)
				return true
			}
		}
		return false
	}
	return true
}
