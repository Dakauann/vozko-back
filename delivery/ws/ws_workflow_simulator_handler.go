package ws

import (
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/domain/metrics"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type WSWorkflowSimulatorHandler struct {
	useCase   workflow_usecase.WSWorkflowSimulationUseCase
	wsMetrics metrics.WSMetricsRecorder
}

func NewWSWorkflowSimulatorHandler(useCase workflow_usecase.WSWorkflowSimulationUseCase, wsMetrics metrics.WSMetricsRecorder) *WSWorkflowSimulatorHandler {
	if useCase == nil || wsMetrics == nil {
		return nil
	}
	return &WSWorkflowSimulatorHandler{useCase: useCase, wsMetrics: wsMetrics}
}

func (h *WSWorkflowSimulatorHandler) HandleSimulate(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.useCase == nil {
		http.Error(w, "workflow simulator not configured", http.StatusNotImplemented)
		return
	}

	vars := mux.Vars(r)
	workflowID := strings.TrimSpace(vars["id"])
	if workflowID == "" {
		http.Error(w, "workflow id is required", http.StatusBadRequest)
		return
	}

	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		http.Error(w, "workspace id is required", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[workflow-sim] failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointWorkflowSimulator)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointWorkflowSimulator)

	log.Printf("[workflow-sim] starting session for workflow=%s workspace=%s", workflowID, workspaceID)

	if err := h.useCase.HandleSession(r.Context(), conn, workflowID, workspaceID); err != nil {
		log.Printf("[workflow-sim] session ended with error: %v", err)
	}
}
