package handlers

import (
	"encoding/json"
	"net/http"
	"strings"

	"vozko/delivery/http/response"
	"vozko/infra/http/middleware"
)

type departmentAssignmentRequest struct {
	DepartmentID string `json:"departmentId"`
}

func decodeDepartmentAssignmentRequest(w http.ResponseWriter, r *http.Request) (string, bool) {
	var req departmentAssignmentRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"departmentId": "string (required)",
		})
		return "", false
	}

	departmentID := strings.TrimSpace(req.DepartmentID)
	if departmentID == "" {
		response.WriteValidationError(w, map[string]string{
			"departmentId": "required",
		})
		return "", false
	}

	return departmentID, true
}

func requirePrivilegedDepartmentAssignment(w http.ResponseWriter, r *http.Request) bool {
	if filter := middleware.GetDepartmentFilter(r); filter != nil && filter.IsOwnerOrAdmin {
		return true
	}

	if claims := middleware.GetClaims(r); claims != nil && claims.Role == "admin" {
		return true
	}

	response.WriteError(w, http.StatusForbidden, "Only owners and admins can assign departments after creation", nil)
	return false
}

func writeDepartmentAlreadyAssignedError(w http.ResponseWriter) {
	response.WriteValidationError(w, map[string]string{
		"departmentId": "resource already has a department",
	})
}
