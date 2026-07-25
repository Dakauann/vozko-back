package ws

import (
	"encoding/json"
	"log"
	"net/http"
	"strings"

	"github.com/gorilla/mux"
	"github.com/gorilla/websocket"

	"vozko/domain/metrics"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"
)

type BuilderRecorderFactory func(workspaceID, workflowID string) *workflow_usecase.BuilderSessionRecorder

type WSWorkflowAIBuilderHandler struct {
	useCase     workflow_usecase.AIBuilderUseCase
	wsMetrics   metrics.WSMetricsRecorder
	newRecorder BuilderRecorderFactory
}

func NewWSWorkflowAIBuilderHandler(useCase workflow_usecase.AIBuilderUseCase, wsMetrics metrics.WSMetricsRecorder, newRecorder BuilderRecorderFactory) *WSWorkflowAIBuilderHandler {
	if useCase == nil || wsMetrics == nil {
		return nil
	}
	return &WSWorkflowAIBuilderHandler{useCase: useCase, wsMetrics: wsMetrics, newRecorder: newRecorder}
}

func (h *WSWorkflowAIBuilderHandler) HandleSession(w http.ResponseWriter, r *http.Request) {
	if h == nil || h.useCase == nil {
		http.Error(w, "workflow ai builder not configured", http.StatusNotImplemented)
		return
	}

	workflowID := strings.TrimSpace(mux.Vars(r)["id"])

	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		http.Error(w, "workspace id is required", http.StatusUnauthorized)
		return
	}

	conn, err := wsUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[workflow-ai-builder] failed to upgrade connection: %v", err)
		return
	}
	defer conn.Close()

	h.wsMetrics.IncWSConnections(metrics.WSEndpointWorkflowAIBuilder)
	defer h.wsMetrics.DecWSConnections(metrics.WSEndpointWorkflowAIBuilder)

	log.Printf("[workflow-ai-builder] starting session for workflow=%q workspace=%s", workflowID, workspaceID)

	var sessionConn workflow_usecase.BuilderConn = conn
	if h.newRecorder != nil {
		if rec := h.newRecorder(workspaceID, workflowID); rec != nil {
			defer rec.Close()
			sessionConn = &recordingConn{Conn: conn, rec: rec}
		}
	}

	if err := h.useCase.HandleSession(r.Context(), sessionConn, workflowID, workspaceID); err != nil {
		log.Printf("[workflow-ai-builder] session ended with error: %v", err)
	}
}

type recordingConn struct {
	*websocket.Conn
	rec *workflow_usecase.BuilderSessionRecorder
}

func (c *recordingConn) ReadMessage() (int, []byte, error) {
	mt, raw, err := c.Conn.ReadMessage()
	if err == nil {
		c.rec.Inbound(raw)
	}
	return mt, raw, err
}

func (c *recordingConn) WriteJSON(v interface{}) error {
	if data, mErr := json.Marshal(v); mErr == nil {
		c.rec.Outbound(data)
	}
	return c.Conn.WriteJSON(v)
}
