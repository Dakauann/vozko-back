package workflowwebhook

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

// RateLimiter is the subset of the rate-limit middleware the public receiver
// needs, so this package does not depend on the concrete middleware type.
type RateLimiter interface {
	Validate(next http.Handler) http.Handler
}

// RegisterRoutes wires the workspace-scoped webhook config lifecycle under the
// protected (authenticated + workspace) router.
func RegisterRoutes(
	protected *mux.Router,
	h *Handler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	res := workspace_domain.ResourceWorkflows
	protected.HandleFunc("/workflows/{id}/webhook", ac(res, workspace_domain.ActionRead, h.GetWorkflowWebhook)).Methods(http.MethodGet)
	protected.HandleFunc("/workflows/{id}/webhook", ac(res, workspace_domain.ActionUpdate, h.CreateWorkflowWebhook)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}/webhook", ac(res, workspace_domain.ActionUpdate, h.UpdateWorkflowWebhook)).Methods(http.MethodPut)
	protected.HandleFunc("/workflows/{id}/webhook/rotate", ac(res, workspace_domain.ActionUpdate, h.RotateWorkflowWebhook)).Methods(http.MethodPost)
	protected.HandleFunc("/workflows/{id}/webhook", ac(res, workspace_domain.ActionUpdate, h.DeleteWorkflowWebhook)).Methods(http.MethodDelete)
}

// RegisterPublicRoutes wires the public webhook receiver, rate limited, on the
// root router.
func RegisterPublicRoutes(public *mux.Router, h *Handler, rl RateLimiter) {
	public.Handle("/webhooks/workflow/{token}", rl.Validate(http.HandlerFunc(h.HandleWebhookTrigger))).Methods(http.MethodPost)
}
