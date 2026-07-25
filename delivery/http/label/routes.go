package label

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *LabelHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	lb := workspace_domain.ResourceLabels
	protected.HandleFunc("/labels", ac(lb, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/labels", ac(lb, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)

	protected.HandleFunc("/labels/reorder", ac(lb, workspace_domain.ActionUpdate, h.Reorder)).Methods(http.MethodPut)
	protected.HandleFunc("/labels/entries", ac(lb, workspace_domain.ActionAssign, h.AssignEntryLabel)).Methods(http.MethodPost)
	protected.HandleFunc("/labels/entries", ac(lb, workspace_domain.ActionAssign, h.RemoveEntryLabel)).Methods(http.MethodDelete)
	protected.HandleFunc("/labels/entries/{entryType}/{entryId}", ac(lb, workspace_domain.ActionRead, h.GetEntryLabels)).Methods(http.MethodGet)

	protected.HandleFunc("/labels/{id}", ac(lb, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/labels/{id}", ac(lb, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}
