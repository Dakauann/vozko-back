package handlers

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net/http"

	"vozko/delivery/http/response"
	"vozko/domain/shared"
	"vozko/domain/workflow"
	workspace_department "vozko/domain/workspace/workspace_department"
	"vozko/infra/http/middleware"
	workflow_usecase "vozko/usecases/workflow"

	"github.com/gorilla/mux"
)

type WorkflowHandler struct {
	createUC           workflow.CreateWorkflowUseCase
	updateUC           workflow.UpdateWorkflowUseCase
	assignDepartmentUC workflow.AssignDepartmentUseCase
	deleteUC           workflow.DeleteWorkflowUseCase
	getUC              workflow.GetWorkflowUseCase
	listUC             workflow.ListWorkflowsUseCase
	activateUC         workflow.ActivateWorkflowUseCase
	pauseUC            workflow.PauseWorkflowUseCase
	startRunUC         workflow.StartRunUseCase
	cancelRunUC        workflow.CancelRunUseCase
	getRunUC           workflow.GetRunUseCase
	listRunsUC         workflow.ListRunsUseCase
	testNodeUC         workflow_usecase.TestNodeUseCase
	catalogFn          func() []workflow.NodeDefinition
}

func NewWorkflowHandler(
	createUC workflow.CreateWorkflowUseCase,
	updateUC workflow.UpdateWorkflowUseCase,
	assignDepartmentUC workflow.AssignDepartmentUseCase,
	deleteUC workflow.DeleteWorkflowUseCase,
	getUC workflow.GetWorkflowUseCase,
	listUC workflow.ListWorkflowsUseCase,
	activateUC workflow.ActivateWorkflowUseCase,
	pauseUC workflow.PauseWorkflowUseCase,
	startRunUC workflow.StartRunUseCase,
	cancelRunUC workflow.CancelRunUseCase,
	getRunUC workflow.GetRunUseCase,
	listRunsUC workflow.ListRunsUseCase,
	testNodeUC workflow_usecase.TestNodeUseCase,
	catalogFn func() []workflow.NodeDefinition,
) *WorkflowHandler {
	return &WorkflowHandler{
		createUC:           createUC,
		updateUC:           updateUC,
		assignDepartmentUC: assignDepartmentUC,
		deleteUC:           deleteUC,
		getUC:              getUC,
		listUC:             listUC,
		activateUC:         activateUC,
		pauseUC:            pauseUC,
		startRunUC:         startRunUC,
		cancelRunUC:        cancelRunUC,
		getRunUC:           getRunUC,
		listRunsUC:         listRunsUC,
		testNodeUC:         testNodeUC,
		catalogFn:          catalogFn,
	}
}

func (h *WorkflowHandler) Create(w http.ResponseWriter, r *http.Request) {
	var req createWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name":         "string (required)",
			"departmentId": "string (optional; required for members who belong to multiple departments)",
			"triggerType":  "string (required)",
		})
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

	created, err := h.createUC.Execute(r.Context(), wf)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *WorkflowHandler) Update(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]

	var req updateWorkflowRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, nil)
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

	updated, err := h.updateUC.Execute(id, wf)
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

	current, err := h.getUC.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	if current.WorkspaceID != middleware.GetWorkspaceID(r) || !canAccessDepartment(r, current.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this workflow", nil)
		return
	}

	updated, err := h.assignDepartmentUC.Execute(withDepartmentCreationScope(r, departmentID).Context(), id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, updated)
}

func (h *WorkflowHandler) Delete(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	if err := h.deleteUC.Execute(id); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusNoContent, nil)
}

func (h *WorkflowHandler) Get(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.getUC.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if !canAccessDepartment(r, wf.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this workflow", nil)
		return
	}

	wf.RequiredCampaignVars = workflow.ExtractCampaignVars(wf.Graph)

	response.WriteSuccess(w, http.StatusOK, wf)
}

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

func (h *WorkflowHandler) Activate(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.activateUC.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) Pause(w http.ResponseWriter, r *http.Request) {
	id := mux.Vars(r)["id"]
	wf, err := h.pauseUC.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, wf)
}

func (h *WorkflowHandler) StartRun(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]

	var req startRunRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"entryId":   "string (required)",
			"entryType": "string (required)",
		})
		return
	}

	input := workflow.StartRunInput{
		WorkflowID:  wfID,
		WorkspaceID: middleware.GetWorkspaceID(r),
		EntryID:     req.EntryID,
		EntryType:   req.EntryType,
	}

	run, err := h.startRunUC.Execute(input)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, run)
}

func (h *WorkflowHandler) CancelRun(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["runId"]
	if err := h.cancelRunUC.Execute(runID); err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "cancelled"})
}

func (h *WorkflowHandler) GetRun(w http.ResponseWriter, r *http.Request) {
	runID := mux.Vars(r)["runId"]
	detail, err := h.getRunUC.Execute(runID)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, detail)
}

func (h *WorkflowHandler) ListRuns(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]
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

	result, err := h.listRunsUC.Execute(input)
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

func (h *WorkflowHandler) NodeTypes(w http.ResponseWriter, r *http.Request) {
	response.WriteSuccess(w, http.StatusOK, h.catalogFn())
}

// ResolveHandles returns, for each posted node, its output handles — including
// config-dependent (dynamic) ones (ai_agent tool routes + response, text_match
// cases) and each handle's optional flag — from the single backend authority.
// The frontend renders these verbatim instead of recomputing handles, so the
// backend is the only source of truth for what is connectable and what is required.
func (h *WorkflowHandler) ResolveHandles(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Nodes []workflow.Node `json:"nodes"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Corpo da requisição inválido", nil)
		return
	}
	catalog := h.catalogFn()
	handles := make(map[string][]workflow.HandleDefinition, len(req.Nodes))
	for _, n := range req.Nodes {
		handles[n.ID] = workflow_usecase.ResolveNodeHandles(catalog, n)
	}
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{"handles": handles})
}

// ValidateGraph runs the full backend lint over a posted graph and returns the
// structured issues + a `valid` flag (the same rules `activate` enforces). The
// frontend calls this to show, before activation, exactly which nodes/handles are
// blocking — instead of guessing or relying on a generic activation error.
func (h *WorkflowHandler) ValidateGraph(w http.ResponseWriter, r *http.Request) {
	var req struct {
		Type  workflow.WorkflowType `json:"type"`
		Graph workflow.Graph        `json:"graph"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Corpo da requisição inválido", nil)
		return
	}
	wf := &workflow.Workflow{Type: req.Type, Graph: req.Graph}
	report := workflow_usecase.LintWorkflowGraph(h.catalogFn(), wf)
	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"valid":  report.IsGreen(),
		"issues": report.Issues,
	})
}

func (h *WorkflowHandler) handleDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, workflow.ErrWorkflowNotFound):
		response.WriteError(w, http.StatusNotFound, "Workflow não encontrado", nil)
	case errors.Is(err, workflow.ErrRunNotFound):
		response.WriteError(w, http.StatusNotFound, "Execução do workflow não encontrada", nil)
	case errors.Is(err, workflow.ErrNameRequired):
		response.WriteValidationError(w, map[string]string{"name": "O nome do workflow é obrigatório"})
	case errors.Is(err, workflow.ErrWorkspaceIDRequired):
		response.WriteError(w, http.StatusUnauthorized, "ID do workspace é obrigatório", nil)
	case errors.Is(err, workflow.ErrInvalidTriggerType):
		response.WriteValidationError(w, map[string]string{"triggerType": "Tipo de gatilho inválido"})
	case errors.Is(err, workspace_department.ErrDepartmentRequired):
		response.WriteValidationError(w, map[string]string{"departmentId": "departmentId é obrigatório quando o usuário pertence a múltiplos departamentos"})
	case errors.Is(err, workspace_department.ErrDepartmentAccessDenied), errors.Is(err, workspace_department.ErrDepartmentNotFound):
		response.WriteValidationError(w, map[string]string{"departmentId": "departmentId inválido ou sem acesso"})
	case errors.Is(err, workflow.ErrWorkflowNotDraft):
		response.WriteError(w, http.StatusConflict, "O workflow precisa estar em rascunho para editar", nil)
	case errors.Is(err, workflow.ErrWorkflowNotActive):
		response.WriteError(w, http.StatusConflict, "O workflow não está ativo", nil)
	case errors.Is(err, workflow.ErrCannotActivate):
		response.WriteError(w, http.StatusConflict, "Não é possível ativar o workflow", nil)
	case errors.Is(err, workflow.ErrInvalidWorkflowType):
		response.WriteError(w, http.StatusBadRequest, "Tipo de workflow inválido", nil)
	case errors.Is(err, workflow.ErrTriggerNotAllowedForType):
		response.WriteError(w, http.StatusBadRequest, "O gatilho selecionado não é compatível com o tipo do workflow", nil)
	case errors.Is(err, workflow.ErrRunAlreadyExists):
		response.WriteError(w, http.StatusConflict, "Já existe uma execução ativa para esta entrada", nil)
	case errors.Is(err, workflow.ErrRunTerminal):
		response.WriteError(w, http.StatusConflict, "A execução já foi finalizada", nil)
	case errors.Is(err, workflow.ErrInsufficientBalance):
		response.WriteError(w, http.StatusPaymentRequired, "Saldo insuficiente", nil)
	case errors.Is(err, workflow.ErrGraphEmpty):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow precisa ter pelo menos um nó", nil)
	case errors.Is(err, workflow.ErrGraphNoTrigger):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow precisa ter um nó de gatilho", nil)
	case errors.Is(err, workflow.ErrGraphMultipleTriggers):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow deve ter apenas um nó de gatilho", nil)
	case errors.Is(err, workflow.ErrGraphDuplicateTriggerType):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow não pode ter dois nós de gatilho do mesmo tipo", nil)
	case errors.Is(err, workflow.ErrGraphTriggerIncompatibleWithType):
		response.WriteError(w, http.StatusUnprocessableEntity, "O nó de gatilho não é compatível com o tipo do workflow", nil)
	case errors.Is(err, workflow.ErrGraphNoEndNode):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow precisa ter pelo menos um nó de Fim", nil)
	case errors.Is(err, workflow.ErrGraphOrphanNode):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow tem nós que não estão conectados", nil)
	case errors.Is(err, workflow.ErrGraphCycleDetected):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow contém um ciclo (loop infinito)", nil)
	case errors.Is(err, workflow.ErrGraphTooManyNodes):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow excede o número máximo de nós", nil)
	case errors.Is(err, workflow.ErrGraphTooManyEdges):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow excede o número máximo de conexões", nil)
	case errors.Is(err, workflow.ErrGraphInvalidEdgeRef):
		response.WriteError(w, http.StatusUnprocessableEntity, "Uma conexão referencia um nó que não existe", nil)
	case errors.Is(err, workflow.ErrGraphDuplicateNodeID):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow tem nós com IDs duplicados", nil)
	case errors.Is(err, workflow.ErrGraphTriggerTypeMismatch):
		response.WriteError(w, http.StatusUnprocessableEntity, "O tipo do nó de gatilho não corresponde ao tipo de gatilho do workflow", nil)
	case errors.Is(err, workflow.ErrGraphNodeNoOutgoing):
		response.WriteError(w, http.StatusUnprocessableEntity, "Todos os nós (exceto Fim) precisam ter pelo menos uma conexão de saída", nil)
	case errors.Is(err, workflow.ErrGraphNodeNoIncoming):
		response.WriteError(w, http.StatusUnprocessableEntity, "Todos os nós (exceto gatilho) precisam ter pelo menos uma conexão de entrada", nil)
	case errors.Is(err, workflow.ErrNodeMissingRequiredField):

		response.WriteError(w, http.StatusUnprocessableEntity, "Campo obrigatório não preenchido em um nó do workflow", map[string]string{"detail": err.Error()})
	case errors.Is(err, workflow.ErrNodeFieldOutOfRange):
		response.WriteError(w, http.StatusUnprocessableEntity, "Um valor numérico em um nó está fora do intervalo permitido.", map[string]string{"detail": err.Error()})
	case errors.Is(err, workflow.ErrNodeInvalidTemplateID):
		response.WriteError(w, http.StatusUnprocessableEntity, "O template selecionado é inválido ou não foi encontrado", nil)
	case errors.Is(err, workflow.ErrNodeInvalidAgentID):
		response.WriteError(w, http.StatusUnprocessableEntity, "O agente selecionado é inválido ou não foi encontrado", nil)
	case errors.Is(err, workflow.ErrNodeInvalidMCPCollectionID):
		response.WriteError(w, http.StatusUnprocessableEntity, "Uma coleção MCP selecionada é inválida ou não foi encontrada", nil)
	case errors.Is(err, workflow.ErrNodeInvalidModelID):
		response.WriteError(w, http.StatusUnprocessableEntity, "O modelo de IA selecionado é inválido ou não está disponível. Use um modelo da lista (find_resource ai_models).", map[string]string{"detail": err.Error()})
	case errors.Is(err, workflow.ErrNodeInvalidToolParamType):
		response.WriteError(w, http.StatusUnprocessableEntity, "Um parâmetro de ferramenta (custom_tools) tem um tipo inválido. Use um tipo suportado (string, number, integer, boolean, array, object, date, time, datetime, email, phone, enum).", map[string]string{"detail": err.Error()})
	case errors.Is(err, workflow.ErrNodeInvalidMediaID):
		response.WriteError(w, http.StatusUnprocessableEntity, "A mídia selecionada é inválida ou não foi encontrada", nil)
	case errors.Is(err, workflow.ErrInvalidNodeType):
		response.WriteError(w, http.StatusUnprocessableEntity, "O workflow contém um tipo de nó inválido", nil)
	case errors.Is(err, workflow.ErrNodeConfigMissing):
		response.WriteError(w, http.StatusUnprocessableEntity, "Configuração obrigatória ausente em um nó do workflow", nil)
	default:
		log.Printf("[workflow_handler] unhandled error: %v", err)
		response.WriteError(w, http.StatusInternalServerError, "Erro interno do servidor", nil)
	}
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
}

type startRunRequest struct {
	EntryID   string `json:"entryId"`
	EntryType string `json:"entryType"`
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
	wf, err := h.getUC.Execute(id)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	if !canAccessDepartment(r, wf.DepartmentID) {
		response.WriteError(w, http.StatusForbidden, "You don't have access to this workflow", nil)
		return
	}

	payload := workflowExportPayload{
		Name:          wf.Name,
		Description:   wf.Description,
		Type:          string(wf.Type),
		TriggerType:   string(wf.TriggerType),
		TriggerConfig: wf.TriggerConfig,
		Graph:         wf.Graph,
		ExportedAt:    wf.UpdatedAt.Format("2006-01-02T15:04:05Z07:00"),
		Version:       wf.Version,
	}

	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Content-Disposition", "attachment; filename=\""+wf.Name+".json\"")
	json.NewEncoder(w).Encode(payload)
}

const maxImportSize = 2 * 1024 * 1024

func (h *WorkflowHandler) Import(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, maxImportSize)

	body, err := io.ReadAll(r.Body)
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Request body too large (max 2 MB)", nil)
		return
	}

	var payload workflowExportPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid JSON", nil)
		return
	}

	if payload.Name == "" {
		response.WriteValidationError(w, map[string]string{"name": "required"})
		return
	}
	triggerType := workflow.TriggerType(payload.TriggerType)

	if payload.TriggerType != "" && !triggerType.Valid() {
		response.WriteValidationError(w, map[string]string{"triggerType": "invalid trigger type"})
		return
	}
	if len(payload.Graph.Nodes) == 0 {
		response.WriteValidationError(w, map[string]string{"graph": "must have at least one node"})
		return
	}

	for _, n := range payload.Graph.Nodes {
		if !n.Type.Valid() {
			response.WriteValidationError(w, map[string]string{"graph": "contains invalid node type: " + string(n.Type)})
			return
		}
	}

	nodeIDs := make(map[string]bool, len(payload.Graph.Nodes))
	for _, n := range payload.Graph.Nodes {
		nodeIDs[n.ID] = true
	}
	for _, e := range payload.Graph.Edges {
		if !nodeIDs[e.Source] || !nodeIDs[e.Target] {
			response.WriteValidationError(w, map[string]string{"graph": "edge references non-existent node"})
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

	created, err := h.createUC.Execute(r.Context(), wf)
	if err != nil {
		h.handleDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, created)
}

func (h *WorkflowHandler) AnalyzeNode(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]
	nodeID := mux.Vars(r)["nodeId"]

	if h.testNodeUC == nil {
		response.WriteError(w, http.StatusNotImplemented, "Test node feature not configured", nil)
		return
	}

	result, err := h.testNodeUC.Analyze(r.Context(), workflow_usecase.AnalyzeNodeInput{
		WorkflowID:  wfID,
		WorkspaceID: middleware.GetWorkspaceID(r),
		NodeID:      nodeID,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result.ToUIAnalysis())
}

func (h *WorkflowHandler) TestNode(w http.ResponseWriter, r *http.Request) {
	wfID := mux.Vars(r)["id"]
	nodeID := mux.Vars(r)["nodeId"]

	if h.testNodeUC == nil {
		response.WriteError(w, http.StatusNotImplemented, "Test node feature not configured", nil)
		return
	}

	var req testNodeRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"mockedState":   "object (optional)",
			"triggerVars":   "object (optional)",
			"skipExecution": "boolean (optional)",
		})
		return
	}

	result, err := h.testNodeUC.Execute(r.Context(), workflow_usecase.TestNodeInput{
		WorkflowID:    wfID,
		WorkspaceID:   middleware.GetWorkspaceID(r),
		NodeID:        nodeID,
		MockedState:   req.MockedState,
		TriggerVars:   req.TriggerVars,
		SkipExecution: req.SkipExecution,
	})
	if err != nil {
		h.handleDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, result)
}

type testNodeRequest struct {
	MockedState   map[string]interface{} `json:"mockedState"`
	TriggerVars   map[string]interface{} `json:"triggerVars"`
	SkipExecution bool                   `json:"skipExecution"`
}
