package httpx

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
)

type DepartmentScopeResolver interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type SenderScope struct {
	WorkspaceID string
	UserID      string
	IsAdmin     bool
	Departments conversation.DepartmentAccessScope
}

func RequireSenderScope(w http.ResponseWriter, r *http.Request, resolver DepartmentScopeResolver) (SenderScope, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteErrorWithCode(w, http.StatusForbidden, "workspace_required", "workspace is required", nil)
		return SenderScope{}, false
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden", "you may not send from this workspace", nil)
		return SenderScope{}, false
	}
	isAdmin := claims.Role == string(user.RoleAdmin)
	if resolver == nil {
		response.WriteErrorWithCode(w, http.StatusInternalServerError, "scope_unavailable",
			"department scope is unavailable, refusing to send", nil)
		return SenderScope{}, false
	}
	departments, allowed := resolver.GetDepartmentScope(claims.UserID, workspaceID, isAdmin)
	if !allowed {
		response.WriteErrorWithCode(w, http.StatusForbidden, "forbidden",
			"you do not have access to this workspace's conversations", nil)
		return SenderScope{}, false
	}
	return SenderScope{WorkspaceID: workspaceID, UserID: claims.UserID, IsAdmin: isAdmin, Departments: departments}, true
}
