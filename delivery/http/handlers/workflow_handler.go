package handlers

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

type WorkflowHandler struct {
	workflows          workflow.ScopedWorkflowsUseCase
	listUC             workflow.ListWorkflowsUseCase
	startRunUC         workflow.StartRunUseCase
	cancelRunUC        workflow.CancelRunUseCase
	getRunUC           workflow.RunDetailPages
	listRunsUC         workflow.ListRunsUseCase
	conversationRunsUC ConversationRunLister
	testNodeUC         workflow_usecase.TestNodeUseCase
	catalogFn          func() []workflow.NodeDefinition
	nodePins           NodePinner
	lastTriggerInput   LastTriggerInputReader
	variables          NodeVariableReader
}

type NodeVariableReader interface {
	At(ctx context.Context, wf *workflow.Workflow, nodeID string) (workflow.VariableCatalog, error)
}

type LastTriggerInputReader interface {
	Execute(viewer shared.Person, wf *workflow.Workflow, nodeID string) (*workflow_usecase.LastTriggerInput, error)
}

type ConversationRunLister interface {
	Execute(viewer shared.Person, workspaceID, entryType, entryID string, pagination shared.Pagination) (*shared.PaginatedResult[*workflow.ConversationRun], error)
}

type NodePinner interface {
	PinFromRun(ctx context.Context, scope workflow.Scope, viewer shared.Person, in workflow_usecase.PinFromRunInput) (*workflow.Workflow, error)
}

func NewWorkflowHandler(
	workflows workflow.ScopedWorkflowsUseCase,
	listUC workflow.ListWorkflowsUseCase,
	startRunUC workflow.StartRunUseCase,
	cancelRunUC workflow.CancelRunUseCase,
	getRunUC workflow.RunDetailPages,
	listRunsUC workflow.ListRunsUseCase,
	conversationRunsUC ConversationRunLister,
	testNodeUC workflow_usecase.TestNodeUseCase,
	catalogFn func() []workflow.NodeDefinition,
	nodePins NodePinner,
	lastTriggerInput LastTriggerInputReader,
	variables NodeVariableReader,
) *WorkflowHandler {
	return &WorkflowHandler{
		workflows:          workflows,
		listUC:             listUC,
		startRunUC:         startRunUC,
		cancelRunUC:        cancelRunUC,
		getRunUC:           getRunUC,
		listRunsUC:         listRunsUC,
		conversationRunsUC: conversationRunsUC,
		testNodeUC:         testNodeUC,
		catalogFn:          catalogFn,
		nodePins:           nodePins,
		lastTriggerInput:   lastTriggerInput,
		variables:          variables,
	}
}

func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody.Expecting(map[string]string{
			"name":         "string (required)",
			"departmentId": "string (optional; required for members who belong to multiple departments)",
			"triggerType":  "string (required)",
		}))
		return
	}

	wf := &workflow.Workflow{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		Name:          req.Name,
		Description:   req.Description,
		Type:          workflow.WorkflowType(req.Type),
		TriggerType:   workflow.TriggerType(req.TriggerType),
		TriggerConfig: req.TriggerConfig,
		Graph:         req.Graph,
	}

	r = withDepartmentCreationScope(r, req.DepartmentID)

	created, err := h.workflows.Create(r.Context(), workflowScope(r), wf)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

// @Summary Salvar o rascunho do workflow
// @Description Grava o rascunho a partir da revisão lida (revision = o campo version do workflow lido). A gravação só acontece se o rascunho guardado ainda está nessa revisão; a resposta traz a nova revisão em version. Se outra sessão (outra pessoa, outra aba ou a Elo) salvou antes, a resposta é 409 draft_conflict com details.revision = revisão guardada agora, e nada é gravado. Sem revision (ou revision < 1) a resposta é 400 draft_revision_required. Salvar nunca muda o que roda: só Publicar muda a versão publicada. Salvar não exige um fluxo completo: gatilho sem conexão, falta de Fim e campos vazios são salvos e só bloqueiam a publicação.
// @Tags workflows
// @Accept json
// @Produce json
// @Param id path string true "ID do workflow"
// @Param body body updateWorkflowRequest true "Rascunho e a revisão em que foi editado"
// @Success 200 {object} workflow.Workflow "Workflow salvo; version é a nova revisão do rascunho"
// @Failure 400 {object} response.ErrorResponse "Corpo inválido ou revision ausente (draft_revision_required)"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Failure 409 {object} response.ErrorResponse{details=draftConflictDetails} "draft_conflict: o rascunho foi salvo por outra sessão; details.revision é a revisão atual"
// @Failure 422 {object} response.ErrorResponse{details=workflowValidationDetails} "workflow_invalid (ou um código específico): o rascunho tem um valor inválido; details.detail traz o texto técnico"
// @Router /workflows/{id} [put]
func (h *WorkflowHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req updateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteRefusal(w, refusalInvalidBody)
		return
	}
	if err := workflow.RequireDraftRevision(req.Revision); err != nil {
		h.handleDomainError(w, err)
		return
	}

	wf := &workflow.Workflow{
		ID:            id,
		Name:          req.Name,
		Description:   req.Description,
		Type:          workflow.WorkflowType(req.Type),
		TriggerType:   workflow.TriggerType(req.TriggerType),
		TriggerConfig: req.TriggerConfig,
		Graph:         req.Graph,
	}

	updated, err := h.workflows.Update(r.Context(), workflowScope(r), id, wf, req.Revision)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *WorkflowHandler) AssignDepartment(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if !requirePrivilegedDepartmentAssignment(w, r) {
		return
	}

	departmentID, ok := decodeDepartmentAssignmentRequest(w, r)
	if !ok {
		return
	}

	updated, err := h.workflows.AssignDepartment(withDepartmentCreationScope(r, departmentID).Context(), workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := h.workflows.Delete(workflowScope(r), id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

// @Summary Obter o workflow para o editor
// @Description Retorna o rascunho (graph) com a revisão em version, que é a revisão a mandar como revision ao salvar; é a leitura a usar para recarregar depois de um 409 draft_conflict. handles traz as saídas atuais de cada nó com saídas dinâmicas (botões e listas, agente com ferramentas, casos de texto, teclas), pela mesma regra de POST /workflows/resolve-handles, para o editor abrir sem outra chamada. entryPoints lista de onde o fluxo recebe execuções, derivado da versão publicada (do rascunho quando nunca foi publicado): cada gatilho e cada ligação externa (webhook, agentes com Executar fluxo, canais, formulários, saídas do lead), com readiness ready ou blocked, o motivo quando bloqueado, os detalhes e a última chamada (lastReceivedAt; no webhook, a última entrega com lastOutcome e lastReason, recusas incluídas). Nenhum detalhe revela uma conversa.
// @Tags workflows
// @Produce json
// @Param id path string true "ID do workflow"
// @Success 200 {object} workflow.Workflow "Workflow com version (revisão do rascunho), handles e entryPoints"
// @Failure 403 {object} response.ErrorResponse "Sem acesso ao departamento do workflow"
// @Failure 404 {object} response.ErrorResponse "Workflow não encontrado"
// @Router /workflows/{id} [get]
func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.workflows.Detail(workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	wf.Handles = workflow_usecase.DynamicHandlesOf(h.catalogFn(), wf.Graph.Nodes)

	response.WriteSuccess(w, http.StatusOK, wf)
}

// @Summary Listar os workflows
// @Description Lista paginada dos workflows do workspace (filtrada pelos departamentos de quem pede). Cada item traz entryPoints (as mesmas entradas do GET /workflows/{id}), carregados para a página inteira num número fixo de consultas. entryKind filtra no banco, antes da paginação (totalItems e totalPages contam só os que passam), os fluxos que têm uma entrada desse tipo em entryPoints, pronta ou bloqueada: um gatilho desse tipo na versão publicada (no rascunho quando nunca foi publicado) ou, para webhook, agent_call e lead_exit, a ligação externa (webhook ativo, agente ativo com Executar fluxo, saída do lead de campanha ou de template).
// @Tags workflows
// @Produce json
// @Param page query int false "Página, a partir de 1"
// @Param pageSize query int false "Itens por página"
// @Param search query string false "Busca pelo nome"
// @Param status query string false "draft, active, paused ou archived"
// @Param type query string false "messages ou voice"
// @Param triggerType query string false "Gatilho (com status=active, o gatilho da versão publicada)"
// @Param entryKind query string false "Tipo de entrada" Enums(message_received, first_message, webhook, form_submitted, agent_call, stage_added, call_received, lead_exit)
// @Success 200 {object} response.PaginatedPayload{data=[]workflow.Workflow}
// @Param startsFromMessage query bool false "true lista só os fluxos que um botão (saída do lead) ou um início pela API conseguem iniciar: os que têm o gatilho Mensagem recebida ou Primeira mensagem na versão publicada (no rascunho quando nunca foi publicado). Filtra no banco, antes da paginação."
// @Failure 400 {object} response.ErrorResponse "invalid_entry_kind: entryKind fora da lista; invalid_filter: startsFromMessage não é true nem false"
// @Router /workflows [get]
func (h *WorkflowHandler) List(w http.ResponseWriter, r *http.Request) {
	values := r.URL.Query()
	options := shared.QueryOptions{
		Pagination: parsePagination(values),
	}

	input := workflow.ListWorkflowsInput{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		DepartmentIDs: departmentFilterIDs(r),
		Search:        values.Get("search"),
		Options:       options,
	}
	if shouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []*workflow.Workflow{}, response.PaginationMeta{
			Page:       options.Pagination.Page,
			PageSize:   options.Pagination.PageSize,
			TotalPages: 0,
			TotalItems: 0,
		})
		return
	}

	if s := values.Get("status"); s != "" {
		status := workflow.WorkflowStatus(s)
		input.Status = &status
	}
	if t := values.Get("triggerType"); t != "" {
		tt := workflow.TriggerType(t)
		input.TriggerType = &tt
	}
	if t := values.Get("type"); t != "" {
		wt := workflow.WorkflowType(t)
		input.Type = &wt
	}
	if raw := values.Get("entryKind"); raw != "" {
		kind, ok := workflow.ParseEntryPointKind(raw)
		if !ok {
			response.WriteRefusal(w, refusalInvalidEntryKind.Expecting(map[string]string{"entryKind": entryKindList()}))
			return
		}
		input.EntryKind = &kind
	}
	if raw := values.Get("startsFromMessage"); raw != "" {
		on, err := strconv.ParseBool(raw)
		if err != nil {
			response.WriteRefusal(w, refusalInvalidFilter.Expecting(map[string]string{"startsFromMessage": "true ou false"}))
			return
		}
		input.StartsFromMessage = on
	}

	result, err := h.listUC.Execute(input)
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

func entryKindList() string {
	kinds := workflow.EntryPointKinds()
	names := make([]string, len(kinds))
	for i, kind := range kinds {
		names[i] = string(kind)
	}
	return strings.Join(names, ", ")
}

func workflowScope(r *http.Request) workflow.Scope {
	return httpx.WorkflowScope(r)
}

type createWorkflowRequest struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	DepartmentID  string                 `json:"departmentId,omitempty"`
	Type          string                 `json:"type"`
	TriggerType   string                 `json:"triggerType"`
	TriggerConfig map[string]interface{} `json:"triggerConfig"`
	Graph         workflow.Graph         `json:"graph"`
}

type updateWorkflowRequest struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description"`
	Type          string                 `json:"type"`
	TriggerType   string                 `json:"triggerType"`
	TriggerConfig map[string]interface{} `json:"triggerConfig"`
	Graph         workflow.Graph         `json:"graph"`
	Revision      int                    `json:"revision" example:"4"`
}

type draftRevisionRequest struct {
	Revision int `json:"revision" example:"4"`
}

type workflowExportPayload struct {
	Name          string                 `json:"name"`
	Description   string                 `json:"description,omitempty"`
	Type          string                 `json:"type"`
	TriggerType   string                 `json:"triggerType"`
	TriggerConfig map[string]interface{} `json:"triggerConfig"`
	Graph         workflow.Graph         `json:"graph"`
	ExportedAt    string                 `json:"exportedAt"`
	Version       int                    `json:"version"`
}

func (h *WorkflowHandler) Export(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.workflows.Get(workflowScope(r), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	payload := workflowExportPayload{
		Name:          wf.Name,
		Description:   wf.Description,
		Type:          string(wf.Type),
		TriggerType:   string(wf.TriggerType),
		TriggerConfig: wf.TriggerConfig,
		Graph:         workflow.StripDraftOnly(wf.Graph),
		ExportedAt:    wf.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Version:       wf.Version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+wf.Name+".json\"")
	json.NewEncoder(w).Encode(payload)
}

const maxImportSize = 2 * 1024 * 1024

func (h *WorkflowHandler) Import(w http.ResponseWriter, r *http.Request) {
	var payload workflowExportPayload
	if !decodeBoundedJSON(w, r, &payload) {
		return
	}

	if payload.Name == "" {
		response.WriteRefusal(w, refusalNameRequired)
		return
	}
	triggerType := workflow.TriggerType(payload.TriggerType)

	if payload.TriggerType != "" && !triggerType.Valid() {
		response.WriteRefusal(w, refusalInvalidTriggerType)
		return
	}
	if len(payload.Graph.Nodes) == 0 {
		response.WriteRefusal(w, refusalGraphRequired)
		return
	}

	for _, n := range payload.Graph.Nodes {
		if !n.Type.Valid() {
			response.WriteRefusal(w, refusalGraphNodeType.Expecting(map[string]string{"graph": "contains invalid node type: " + string(n.Type)}))
			return
		}
	}

	nodeIDs := make(map[string]bool, len(payload.Graph.Nodes))
	for _, n := range payload.Graph.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, e := range payload.Graph.Edges {
		if !nodeIDs[e.Source] || !nodeIDs[e.Target] {
			response.WriteRefusal(w, refusalGraphEdgeRef)
			return
		}
	}

	wfType := workflow.WorkflowType(payload.Type)
	if !wfType.Valid() {
		if primary := payload.Graph.PrimaryTriggerType(); primary != "" {
			wfType = primary.WorkflowType()
		} else if triggerType.Valid() {
			wfType = triggerType.WorkflowType()
		} else {
			wfType = workflow.WorkflowTypeMessages
		}
	}

	if err := workflow.ValidateGraph(&payload.Graph, wfType); err != nil {
		h.handleDomainError(w, err)
		return
	}

	if payload.TriggerConfig == nil {
		payload.TriggerConfig = make(map[string]interface{})
	}

	wf := &workflow.Workflow{
		WorkspaceID:   middleware.GetWorkspaceID(r),
		Name:          payload.Name,
		Description:   payload.Description,
		Type:          wfType,
		TriggerType:   triggerType,
		TriggerConfig: payload.TriggerConfig,
		Graph:         payload.Graph,
	}

	r = withDepartmentCreationScope(r, r.URL.Query().Get("departmentId"))

	created, err := h.workflows.Create(r.Context(), workflowScope(r), wf)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}
