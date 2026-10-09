package httpx

import (
	"net/http"
	"strings"

	"vozko/domain/conversation"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	lead_usecase "vozko/usecases/lead"
)

func WorkspaceActor(r *http.Request) (conversation.Viewer, bool) {
	workspaceID := strings.TrimSpace(middleware.GetWorkspaceID(r))
	claims := middleware.GetClaims(r)
	if workspaceID == "" || claims == nil || strings.TrimSpace(claims.UserID) == "" {
		return conversation.Viewer{}, false
	}
	return conversation.Viewer{
		UserID:      strings.TrimSpace(claims.UserID),
		WorkspaceID: workspaceID,
		IsAdmin:     claims.Role == string(user.RoleAdmin),
	}, true
}

func LeadActor(r *http.Request) (lead_usecase.Actor, bool) {
	return WorkspaceActor(r)
}
