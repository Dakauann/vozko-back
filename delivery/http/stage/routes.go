package stage

import (
	"net/http"

	"github.com/gorilla/mux"

	workspace_domain "vozko/domain/workspace"
)

func RegisterRoutes(
	protected *mux.Router,
	h *StageHandler,
	ac func(workspace_domain.Resource, workspace_domain.Action, http.HandlerFunc) http.HandlerFunc,
) {
	tg := workspace_domain.ResourceStages
	protected.HandleFunc("/stages", ac(tg, workspace_domain.ActionRead, h.List)).Methods(http.MethodGet)
	protected.HandleFunc("/stages", ac(tg, workspace_domain.ActionCreate, h.Create)).Methods(http.MethodPost)

	protected.HandleFunc("/stages/initial", ac(tg, workspace_domain.ActionUpdate, h.SetInitialStage)).Methods(http.MethodPut)
	protected.HandleFunc("/stages/reorder", ac(tg, workspace_domain.ActionUpdate, h.Reorder)).Methods(http.MethodPut)
	protected.HandleFunc("/stages/entries", ac(tg, workspace_domain.ActionAssign, h.AssignEntryStage)).Methods(http.MethodPost)
	protected.HandleFunc("/stages/entries", ac(tg, workspace_domain.ActionAssign, h.RemoveEntryStage)).Methods(http.MethodDelete)
	protected.HandleFunc("/stages/entries/batch", ac(tg, workspace_domain.ActionRead, h.GetBatchEntryStages)).Methods(http.MethodPost)
	protected.HandleFunc("/stages/entries/{entryType}/{entryId}", ac(tg, workspace_domain.ActionRead, h.GetEntryStage)).Methods(http.MethodGet)

	protected.HandleFunc("/stages/{id}", ac(tg, workspace_domain.ActionUpdate, h.Update)).Methods(http.MethodPut)
	protected.HandleFunc("/stages/{id}", ac(tg, workspace_domain.ActionDelete, h.Delete)).Methods(http.MethodDelete)
}
