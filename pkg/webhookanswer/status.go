package webhookanswer

import "net/http"

var rejectionStatuses = map[string]int{
	"webhook_inactive":      http.StatusNotFound,
	"webhook_not_found":     http.StatusNotFound,
	"method_not_allowed":    http.StatusMethodNotAllowed,
	"unauthorized":          http.StatusUnauthorized,
	"entry_required":        http.StatusBadRequest,
	"entry_not_found":       http.StatusNotFound,
	"entry_forbidden":       http.StatusForbidden,
	"no_trigger_node":       http.StatusUnprocessableEntity,
	"workflow_not_active":   http.StatusConflict,
	"workspace_at_capacity": http.StatusTooManyRequests,
}

func RejectionStatus(reason string) int {
	if status, ok := rejectionStatuses[reason]; ok {
		return status
	}
	return http.StatusInternalServerError
}

func AcceptStatus(outcome string) int {
	if outcome == "accepted" {
		return http.StatusAccepted
	}
	return http.StatusOK
}
