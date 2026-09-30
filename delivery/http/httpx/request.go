package httpx

import (
	"encoding/json"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
)

func RequireWorkspace(w http.ResponseWriter, r *http.Request) (string, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", false
	}
	return workspaceID, true
}

func DecodeJSON(w http.ResponseWriter, r *http.Request, into any) bool {
	if err := json.NewDecoder(r.Body).Decode(into); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return false
	}
	return true
}
