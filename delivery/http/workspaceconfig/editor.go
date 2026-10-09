package workspaceconfig

import (
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/user"
	wsc "vozko/domain/workspace_config"
	"vozko/infra/http/middleware"
)

func sessionCaller(w http.ResponseWriter, r *http.Request) (wsc.Caller, bool) {
	claims := middleware.GetClaims(r)
	if claims == nil || claims.UserID == "" {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return wsc.Caller{}, false
	}
	return wsc.Caller{UserID: claims.UserID, PlatformAdmin: claims.Role == string(user.RoleAdmin)}, true
}
