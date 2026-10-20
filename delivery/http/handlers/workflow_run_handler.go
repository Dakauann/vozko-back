package handlers

import (
	"encoding/json"
	"errors"
	"net/http"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"

	"github.com/gorilla/mux"
)

type startRunRequest struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
}

// @Summary Iniciar uma execução de workflow
// @Description Inicia manualmente uma execução da versão publicada do workflow para uma conversa (entryId e entryType) do workspace atual, e a execução começa a rodar na hora. Exige acesso ao departamento do workflow e que quem pede possa ver a conversa (mesma regra de departamento e de responsável da caixa de entrada); caso contrário a resposta é 403 ou 404.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param body body startRunRequest true "Conversa em que a execução roda"
// @Success 201 {object} workflow.WorkflowRun
// @Failure 400 {object} response.ErrorResponse "Corpo inválido"
// @Failure 401 {object} response.ErrorResponse
// @Failure 403 {object} response.ErrorResponse "Workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "Workflow ou conversa não encontrados"
// @Failure 409 {object} response.ErrorResponse "Workflow inativo ou execução já em andamento"
// @Router /workflows/{id}/runs [post]
func (h *WorkflowHandler) StartRun(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	wfID := mux.Vars(r)["id"]
	if _, err := h.workflows.Get(workflowScope(r), wfID); err != nil {
		h.handleDomainError(w, err)
		return
	}

	var req startRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{
			"entryId":   "string (required)",
			"entryType": "string (required)",
		}))
		return
	}

	input := workflow.StartRunInput{
		WorkflowID:  wfID,
		WorkspaceID: middleware.GetWorkspaceID(r),
		EntryID:     req.EntryID,
		EntryType:   req.EntryType,
		StartedBy:   workflow.StartedByManual(viewer.UserID),
		Viewer:      viewer,
	}

	run, err := h.startRunUC.Execute(input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, run)
}

// @Summary Cancelar uma execução de workflow
// @Description Cancela uma execução ainda ativa. Só encontra execuções do workspace atual em conversas que quem pede pode ver (mesma regra de departamento e de responsável da caixa de entrada); fora disso a resposta é 404.
// @Tags workflows
// @Produce json
// @Param runId path string true "ID da execução"
// @Success 200 {object} map[string]string "status: cancelled"
// @Failure 404 {object} response.ErrorResponse "Execução não encontrada"
// @Failure 409 {object} response.ErrorResponse "A execução já terminou"
// @Router /workflow-runs/{runId}/cancel [post]
func (h *WorkflowHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	runID := mux.Vars(r)["runId"]
	if err := h.cancelRunUC.Execute(viewer, middleware.GetWorkspaceID(r), runID); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

// @Summary Obter uma execução de workflow
// @Description Retorna a execução com seu estado e os registros das etapas em logs, em ordem de execução e paginados: sem parâmetros vêm as primeiras 500 etapas (todas, numa execução comum). steps traz offset, limit, total e hasMore; com hasMore, peça a próxima página com stepsOffset = offset + limit. stepsLimit vai até 1000. Só encontra execuções do workspace atual em conversas que quem pede pode ver (mesma regra de departamento e de responsável da caixa de entrada); fora disso a resposta é 404.
// @Tags workflows
// @Produce json
// @Param runId path string true "ID da execução"
// @Param stepsOffset query int false "Primeira etapa a devolver, a partir de 0"
// @Param stepsLimit query int false "Quantas etapas devolver (padrão 500, máximo 1000)"
// @Success 200 {object} workflow.RunDetail
// @Failure 400 {object} response.ErrorResponse "stepsOffset ou stepsLimit inválido"
// @Failure 404 {object} response.ErrorResponse "Execução não encontrada"
// @Router /workflow-runs/{runId} [get]
func (h *WorkflowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	window, ok := stepWindowOf(r.URL.Query())
	if !ok {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{"stepsOffset": "int >= 0 (optional)", "stepsLimit": "int >= 1 (optional, até 1000)"}))
		return
	}
	runID := mux.Vars(r)["runId"]
	detail, err := h.getRunUC.ExecutePage(viewer, middleware.GetWorkspaceID(r), runID, window)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, detail)
}

// @Summary Listar execuções de um workflow
// @Description Lista as execuções do workflow no workspace atual, da mais recente para a mais antiga, com filtro opcional por conversa e status. Só entram execuções em conversas que quem pede pode ver (mesma regra de departamento e de responsável da caixa de entrada), filtradas na própria consulta, então as páginas ficam completas.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param entryId query string false "Filtra pela conversa"
// @Param status query string false "Filtra pelo status (running, waiting, completed, failed, cancelled)"
// @Param page query int false "Página, a partir de 1"
// @Param pageSize query int false "Itens por página"
// @Success 200 {object} response.PaginatedPayload{data=[]workflow.WorkflowRun}
// @Failure 403 {object} response.ErrorResponse "Workflow de outro departamento"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Router /workflows/{id}/runs [get]
func (h *WorkflowHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	wfID := mux.Vars(r)["id"]
	if _, err := h.workflows.Get(workflowScope(r), wfID); err != nil {
		h.handleDomainError(w, err)
		return
	}
	values := r.URL.Query()

	input := workflow.ListRunsInput{
		WorkspaceID: middleware.GetWorkspaceID(r),
		WorkflowID:  wfID,
		EntryID:     values.Get("entryId"),
		Options: shared.QueryOptions{
			Pagination: parsePagination(values),
		},
	}
	if s := values.Get("status"); s != "" {
		status := workflow.RunStatus(s)
		input.Status = &status
	}

	result, err := h.listRunsUC.Execute(viewer, input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary Listar as execuções de fluxo de uma conversa
// @Description Lista as execuções de todos os fluxos nesta conversa, da mais recente para a mais antiga, paginado. Cada item traz o nome do fluxo, a versão em que a execução roda, o status, o tipo de gatilho, quem iniciou (startedBy: agent:<id>, webhook:<id>, exit:<id>, manual:<id> ou trigger:<tipo>), o início, o fim e a duração em ms (só para execuções encerradas). Enquanto a execução está rodando ou aguardando, traz também a etapa atual (currentNodeId, currentNodeType e currentNodeName) e o motivo da espera. Exige `workflows:read` e que quem pede possa ver a conversa pela mesma regra de departamento e de responsável da caixa de entrada; caso contrário a resposta é 404.
// @Tags workflows
// @Produce json
// @Param entryType path string true "Tipo da entrada (whatsapp, instagram, facebook, telegram, webchat, mercadolivre, tiktok, unofficial_whatsapp)"
// @Param entryId path string true "ID da entrada"
// @Param page query int false "Página, a partir de 1"
// @Param pageSize query int false "Itens por página"
// @Success 200 {object} response.PaginatedPayload{data=[]workflow.ConversationRun}
// @Failure 401 {object} response.ErrorResponse
// @Failure 404 {object} response.ErrorResponse "Conversa não encontrada"
// @Failure 500 {object} response.ErrorResponse "Erro ao listar as execuções"
// @Security BearerAuth
// @Router /conversations/{entryType}/{entryId}/workflow-runs [get]
func (h *WorkflowHandler) ListConversationRuns(w http.ResponseWriter, r *http.Request) {
	viewer, ok := httpx.RequirePerson(w, r)
	if !ok {
		return
	}
	vars := mux.Vars(r)
	result, err := h.conversationRunsUC.Execute(viewer, middleware.GetWorkspaceID(r), vars["entryType"], vars["entryId"], httpx.ParsePagination(r.URL.Query()))
	switch {
	case errors.Is(err, conversation.ErrNotVisible):
		response.WriteRefusal(w, refusalConversationHidden)
		return
	case err != nil:
		response.WriteRefusal(w, refusalConversationRuns)
		return
	}
	response.WritePaginated(w, http.StatusOK, result.Items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}
