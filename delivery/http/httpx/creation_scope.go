package httpx

import (
	"net/http"
	"strings"

	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
)

func WithCreationScope(r *http.Request, explicitDepartmentID string) *http.Request {
	if r == nil {
		return nil
	}
	requestedDepartmentID := strings.TrimSpace(explicitDepartmentID)
	if requestedDepartmentID == "" {
		requestedDepartmentID = strings.TrimSpace(middleware.SelectedDepartmentID(r))
	}
	scope := workspace_department.CreationScope{RequestedDepartmentID: requestedDepartmentID}
	if claims := middleware.GetClaims(r); claims != nil {
		scope.UserID = claims.UserID
		scope.IsSystemAdmin = claims.Role == "admin"
	}
	return r.WithContext(workspace_department.WithCreationScope(r.Context(), scope))
}
