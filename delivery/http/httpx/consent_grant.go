package httpx

import (
	"errors"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/domain/user"
	"vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

func RequesterOf(r *http.Request) workspace.Requester {
	claims := middleware.GetClaims(r)
	if claims == nil {
		return workspace.Requester{}
	}
	return workspace.Requester{UserID: strings.TrimSpace(claims.UserID), SystemAdmin: claims.Role == string(user.RoleAdmin)}
}

func WriteConsentGrantRefusal(w http.ResponseWriter, err error) bool {
	if !errors.Is(err, workspace.ErrConsentGrantForbidden) {
		return false
	}
	permission := workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionConsent}.Key()
	response.WriteErrorWithCode(w, http.StatusForbidden, workspace.CodeConsentGrantForbidden,
		"Registrar consentimento de leads exige a permissão "+permission, map[string]string{"permission": permission})
	return true
}
