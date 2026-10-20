package tiktok

import (
	"errors"
	"log"
	"net/http"

	"vozko/delivery/http/oauthpopup"
	"vozko/delivery/http/response"
	ttdomain "vozko/domain/tiktok"
	"vozko/usecases/shared/automationrefs"
	ttuc "vozko/usecases/tiktok"
)

type RateLimitDetector func(err error) bool

func writeDomainError(w http.ResponseWriter, err error, fallback string, rateLimited RateLimitDetector) {
	switch {
	case errors.Is(err, ttdomain.ErrAccountNotFound), errors.Is(err, ttdomain.ErrCommentNotFound),
		errors.Is(err, ttdomain.ErrCommentNotOwned), errors.Is(err, ttdomain.ErrVideoNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)
	case errors.Is(err, ttdomain.ErrAccountAlreadyLinked):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)
	case errors.Is(err, ttuc.ErrAccountUnavailable), errors.Is(err, ttdomain.ErrNeedsReconnect), errors.Is(err, ttdomain.ErrAccountRevoked):
		response.WriteError(w, http.StatusConflict, "A conta do TikTok não está conectada ou não tem a permissão para isso", nil)
	case errors.Is(err, ttdomain.ErrCommentTextEmpty), errors.Is(err, ttdomain.ErrCommentTextTooLong),
		errors.Is(err, ttuc.ErrDisclosureTooLong), errors.Is(err, automationrefs.ErrNotInWorkspace):
		response.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)
	case errors.Is(err, ttdomain.ErrWorkspaceIDRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
	case rateLimited != nil && rateLimited(err):
		response.WriteError(w, http.StatusTooManyRequests, "O TikTok limitou as chamadas desta conta; tente de novo em instantes", nil)
	default:
		log.Printf("[tiktok] %s: %v", fallback, err)
		response.WriteError(w, http.StatusBadGateway, fallback, nil)
	}
}

func connectErrorCode(err error) string {
	if code, ok := oauthpopup.StateErrorCode(err); ok {
		return code
	}
	switch {
	case errors.Is(err, ttuc.ErrAuthorizationDeclined):
		return "declined"
	case errors.Is(err, ttuc.ErrAuthorizationCode):
		return "code_missing"
	case errors.Is(err, ttdomain.ErrScopeMissing):
		return "scope_missing"
	case errors.Is(err, ttdomain.ErrNotEligibleAccount):
		return "not_eligible_account"
	case errors.Is(err, ttdomain.ErrAccountAlreadyLinked):
		return "already_linked"
	case errors.Is(err, ttdomain.ErrIdentityMismatch):
		return "identity_mismatch"
	case errors.Is(err, ttuc.ErrProbeInconclusive):
		return "probe_inconclusive"
	}
	return "connect_failed"
}
