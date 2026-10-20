package workflowalerthttp

import (
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/workflow"
)

type Alerts interface {
	ForWorkflow(viewer shared.Person, workspaceID, workflowID string) ([]workflow.RuntimeAlert, error)
	OpenInWorkspace(viewer shared.Person, scope workflow.Scope, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.RuntimeAlert], error)
}

type WorkflowAccess = httpx.WorkflowAccess

var (
	refusalWorkflowAlertsFailed  = response.Refusal{Status: http.StatusInternalServerError, Code: "workflow_alerts_failed", Message: "Erro ao listar os alertas do workflow"}
	refusalWorkspaceAlertsFailed = response.Refusal{Status: http.StatusInternalServerError, Code: "workspace_alerts_failed", Message: "Erro ao listar os alertas"}
)

var alertRefusals = []response.Refusal{refusalWorkflowAlertsFailed, refusalWorkspaceAlertsFailed}

type Handler struct {
	alerts    Alerts
	workflows WorkflowAccess
}

func NewHandler(alerts Alerts, workflows WorkflowAccess) *Handler {
	return &Handler{alerts: alerts, workflows: workflows}
}

// @Summary		Listar os alertas de execução de um workflow
// @Description	Lista os alertas de execução do workflow que estão abertos e os resolvidos nas últimas 24 horas, do mais recente para o mais antigo. Os alertas são calculados a cada 2 minutos a partir das execuções, das chamadas ao webhook e das chamadas de ferramentas dos agentes: workflow_called_inactive (chamado pelo webhook ou por um agente enquanto não está ativo ou nunca foi publicado), node_failing (um nó falhou em pelo menos 3 das suas últimas 10 execuções nas últimas 24 horas), webhook_rejecting (pelo menos 5 recusas do webhook pelo mesmo motivo em 1 hora, motivo em subject), run_stuck (uma execução ficou parada e foi encerrada com erro), agent_tool_failing (a ferramenta subject de um agente falhou pelo menos 3 vezes em 1 hora). fixAction diz o que fazer: publish, open_run (abrir runId e selecionar nodeId), open_node, view_deliveries, open_agent (agentId) ou open_conversation (entryId e entryType). Cada alerta aberto também gera um aviso na central de notificações para quem pode editar workflows. Exige `workflows:read`.
// @Tags			workflows
// @Produce		json
// @Param			id	path		string	true	"ID do workflow"
// @Success		200	{object}	WorkflowAlertsResponse
// @Failure		403	{object}	response.ErrorResponse	"Workflow de outro departamento"
// @Failure		404	{object}	response.ErrorResponse	"Workflow não encontrado"
// @Failure		500	{object}	response.ErrorResponse	"Erro ao listar os alertas"
// @Security		BearerAuth
// @Router			/workflows/{id}/alerts [get]
func (h *Handler) ListForWorkflow(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	workflowID := mux.Vars(r)["id"]
	if !httpx.RequireWorkflowAccess(w, r, h.workflows, workflowID) {
		return
	}
	alerts, err := h.alerts.ForWorkflow(viewer, httpx.WorkflowScope(r).WorkspaceID, workflowID)
	if err != nil {
		response.WriteRefusal(w, refusalWorkflowAlertsFailed)
		return
	}
	out := WorkflowAlertsResponse{Items: make([]AlertResponse, len(alerts))}
	for i, a := range alerts {
		out.Items[i] = toResponse(a)
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Listar os alertas de execução abertos do workspace
// @Description	Lista os alertas de execução abertos de todos os workflows do workspace, do visto mais recentemente para o mais antigo, paginado. Inclui inbound_lookup_failed (mensagens que ficaram sem resposta automática porque não foi possível verificar se algum fluxo esperava a resposta), que não pertence a um workflow e só aparece para quem pode ver a conversa que ele aponta (mesma regra de departamento e de responsável da caixa de entrada). Quem só enxerga alguns departamentos vê apenas os alertas dos workflows desses departamentos. Os campos são os mesmos de GET /workflows/{id}/alerts. Exige `workflows:read`.
// @Tags			workflows
// @Produce		json
// @Param			page		query		int	false	"Página, a partir de 1"
// @Param			pageSize	query		int	false	"Itens por página"
// @Success		200	{object}	response.PaginatedPayload{data=[]AlertResponse}
// @Failure		500	{object}	response.ErrorResponse	"Erro ao listar os alertas"
// @Security		BearerAuth
// @Router			/workflow-alerts [get]
func (h *Handler) ListOpen(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	result, err := h.alerts.OpenInWorkspace(viewer, httpx.WorkflowScope(r), httpx.ParsePagination(r.URL.Query()))
	if err != nil {
		response.WriteRefusal(w, refusalWorkspaceAlertsFailed)
		return
	}
	items := make([]AlertResponse, len(result.Items))
	for i, a := range result.Items {
		items[i] = toResponse(*a)
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}
