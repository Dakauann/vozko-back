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
	return writeCapabilityRefusal(w, err, workspace.ErrConsentGrantForbidden, workspace.CodeConsentGrantForbidden,
		"Registrar consentimento de leads exige a permissão ", workspace.PermissionEntry{Resource: workspace.ResourceLeads, Action: workspace.ActionConsent})
}

func WriteSheetBindingRefusal(w http.ResponseWriter, err error) bool {
	return writeCapabilityRefusal(w, err, workspace.ErrSheetBindingForbidden, workspace.CodeSheetBindingForbidden,
		"Dar uma planilha a um agente exige a permissão ", workspace.PermissionEntry{Resource: workspace.ResourceGoogleSheets, Action: workspace.ActionRead})
}

func writeCapabilityRefusal(w http.ResponseWriter, err, sentinel error, code, message string, needed workspace.PermissionEntry) bool {
	if !errors.Is(err, sentinel) {
		return false
	}
	permission := needed.Key()
	response.WriteErrorWithCode(w, http.StatusForbidden, code, message+permission, map[string]string{"permission": permission})
	return true
}
