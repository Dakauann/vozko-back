package handlers

import (
	"encoding/json"
	"net/http"
	"strconv"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

// @Summary Publicar o rascunho do workflow
// @Description Valida o rascunho com todas as regras que bloqueiam (avisos não bloqueiam), grava uma nova versão publicada com o próximo número e deixa o workflow ativo. Novas execuções passam a usar essa versão; execuções em andamento continuam na versão em que começaram. Se o rascunho é igual à versão publicada, nenhuma versão nova é criada. Quando o rascunho não pode ser publicado, a resposta 422 workflow_not_publishable lista em details.issues todos os problemas que bloqueiam (as mesmas regras do painel Alertas em POST /workflows/validate, mais as referências conferidas no banco: agente, template, etiqueta, etapa, funil, departamento, atendente, modelo, workflow, número, mídia, MCP e base de conhecimento). Avisos nunca bloqueiam.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Success 200 {object} workflow.Workflow "Workflow com publishedVersion, publishedAt e hasUnpublishedChanges=false"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Failure 409 {object} response.ErrorResponse "Workflow arquivado ou publicação simultânea"
// @Failure 422 {object} response.ErrorResponse{details=workflowNotPublishableDetails} "workflow_not_publishable: details.issues lista todos os problemas que bloqueiam, cada um com code, severity, nodeId, path e message"
// @Router /workflows/{id}/publish [post]
func (h *WorkflowHandler) Publish(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.workflows.Publish(r.Context(), workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

// @Summary Listar as versões publicadas do workflow
// @Description Lista as versões publicadas, da mais recente para a mais antiga, sem o grafo de cada uma.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param page query int false "Página, a partir de 1"
// @Param pageSize query int false "Itens por página"
// @Success 200 {object} response.PaginatedPayload{data=[]workflow.WorkflowVersion}
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Router /workflows/{id}/versions [get]
func (h *WorkflowHandler) ListVersions(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	result, err := h.workflows.Versions(workflowScope(r), id, shared.QueryOptions{Pagination: parsePagination(r.URL.Query())})
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

// @Summary Restaurar uma versão no rascunho
// @Description Copia o grafo da versão escolhida para o rascunho, com as mesmas validações e a mesma checagem de revisão de salvar (revision = o version do rascunho que a pessoa está vendo). Não publica: a versão publicada continua rodando até a próxima publicação.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param version path int true "Número da versão"
// @Param body body draftRevisionRequest true "Revisão do rascunho que será substituído"
// @Success 200 {object} workflow.Workflow "Workflow com o rascunho restaurado; version é a nova revisão"
// @Failure 400 {object} response.ErrorResponse "Número de versão inválido ou revision ausente (draft_revision_required)"
// @Failure 404 {object} response.ErrorResponse "Workflow ou versão não encontrados"
// @Failure 409 {object} response.ErrorResponse{details=draftConflictDetails} "draft_conflict: o rascunho foi salvo por outra sessão"
// @Router /workflows/{id}/versions/{version}/restore [post]
func (h *WorkflowHandler) RestoreVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	version, err := strconv.Atoi(mux.Vars(r)["version"])
	if err != nil {
		h.handleDomainError(w, workflow.ErrInvalidVersion)
		return
	}
	var req draftRevisionRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{"revision": "int (required)"}))
		return
	}
	if err := workflow.RequireDraftRevision(req.Revision); err != nil {
		h.handleDomainError(w, err)
		return
	}
	wf, err := h.workflows.RestoreVersion(r.Context(), workflowScope(r), id, version, req.Revision)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

// @Summary Obter uma versão publicada do workflow
// @Description Retorna a versão com o grafo exatamente como foi publicado (sem saídas fixadas) e handles, as saídas atuais dos nós com saídas dinâmicas pela regra de POST /workflows/resolve-handles, para mostrar uma execução na versão em que ela rodou ou para comparar com o rascunho.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Param version path int true "Número da versão"
// @Success 200 {object} workflow.WorkflowVersion "Versão com o grafo"
// @Failure 400 {object} response.ErrorResponse "Número de versão inválido"
// @Failure 404 {object} response.ErrorResponse "Workflow ou versão não encontrados"
// @Router /workflows/{id}/versions/{version} [get]
func (h *WorkflowHandler) GetVersion(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	version, err := strconv.Atoi(mux.Vars(r)["version"])
	if err != nil {
		h.handleDomainError(w, workflow.ErrInvalidVersion)
		return
	}
	v, err := h.workflows.Version(workflowScope(r), id, version)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	if v.Graph != nil {
		v.Handles = workflow_usecase.DynamicHandlesOf(h.catalogFn(), v.Graph.Nodes)
	}
	response.WriteSuccess(w, http.StatusOK, v)
}

// @Summary Ativar o workflow
// @Description Sem versão publicada, publica o rascunho (como Publicar). Com versão publicada e pausado, volta a rodar a versão publicada, sem publicar o rascunho. Se a versão a rodar tem problemas que bloqueiam, a resposta 422 workflow_not_publishable lista todos em details.issues.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Success 200 {object} workflow.Workflow
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Failure 409 {object} response.ErrorResponse "O workflow já está ativo ou arquivado"
// @Failure 422 {object} response.ErrorResponse{details=workflowNotPublishableDetails} "workflow_not_publishable: details.issues lista todos os problemas que bloqueiam"
// @Router /workflows/{id}/activate [post]
func (h *WorkflowHandler) Activate(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.workflows.Activate(r.Context(), workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.workflows.Pause(workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}
