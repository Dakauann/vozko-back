package unofficial_whatsapp

import (
	"encoding/json"
	"errors"
	"net/http"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/conversation"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	user_domain "vozko/domain/user"
	"vozko/infra/http/middleware"
	uwuc "vozko/usecases/unofficial_whatsapp"
)

type Handler struct {
	provision   *uwuc.ProvisionInstanceUseCase
	connect     *uwuc.ConnectInstanceUseCase
	list        *uwuc.ListInstancesUseCase
	get         *uwuc.GetInstanceUseCase
	updateCfg   *uwuc.UpdateInstanceConfigUseCase
	rotateToken *uwuc.RotateDeliveryTokenUseCase
	remove      *uwuc.DeleteInstanceUseCase
	startConv   *uwuc.StartConversationUseCase
	allowance   *uwuc.GetAllowanceUseCase
	departments DepartmentScopeResolver
}

type DepartmentScopeResolver interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

type HandlerDeps struct {
	Provision   *uwuc.ProvisionInstanceUseCase
	Connect     *uwuc.ConnectInstanceUseCase
	List        *uwuc.ListInstancesUseCase
	Get         *uwuc.GetInstanceUseCase
	UpdateCfg   *uwuc.UpdateInstanceConfigUseCase
	RotateToken *uwuc.RotateDeliveryTokenUseCase
	Remove      *uwuc.DeleteInstanceUseCase
	StartConv   *uwuc.StartConversationUseCase
	Allowance   *uwuc.GetAllowanceUseCase
	Departments DepartmentScopeResolver
}

func NewHandler(d HandlerDeps) *Handler {
	return &Handler{
		provision:   d.Provision,
		startConv:   d.StartConv,
		connect:     d.Connect,
		list:        d.List,
		get:         d.Get,
		updateCfg:   d.UpdateCfg,
		rotateToken: d.RotateToken,
		remove:      d.Remove,
		allowance:   d.Allowance,
		departments: d.Departments,
	}
}

func (h *Handler) scopeFor(r *http.Request) (uw.DepartmentScope, bool) {
	if h.departments == nil {
		return uw.Unrestricted(), true
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		return uw.DepartmentScope{}, false
	}
	workspaceID := middleware.GetWorkspaceID(r)

	scope, allowed := h.departments.GetDepartmentScope(
		claims.UserID, workspaceID, claims.Role == string(user_domain.RoleAdmin))
	if !allowed {
		return uw.DepartmentScope{}, false
	}
	return uw.DepartmentScope{
		DepartmentIDs: scope.DepartmentIDs,
		Restrict:      scope.Restrict,
	}, true
}

func (h *Handler) requireScope(w http.ResponseWriter, r *http.Request) (string, uw.DepartmentScope, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", uw.DepartmentScope{}, false
	}
	scope, allowed := h.scopeFor(r)
	if !allowed {
		response.WriteError(w, http.StatusForbidden,
			"you do not have access to this workspace's numbers", nil)
		return "", uw.DepartmentScope{}, false
	}
	return workspaceID, scope, true
}

type allowanceDTO struct {
	Limit      int  `json:"limit"`
	Used       int  `json:"used"`
	Granted    int  `json:"granted"`
	Purchased  int  `json:"purchased"`
	Remaining  int  `json:"remaining"`
	CanConnect bool `json:"canConnect"`
	OverLimit  bool `json:"overLimit"`
}

func (h *Handler) GetAllowance(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}

	allowance, err := h.allowance.Execute(r.Context(), workspaceID)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, allowanceDTO{
		Limit:      allowance.Limit,
		Used:       allowance.Used,
		Granted:    allowance.Granted,
		Purchased:  allowance.Purchased,
		Remaining:  allowance.Remaining(),
		CanConnect: allowance.CanProvision(),
		OverLimit:  allowance.OverLimit(),
	})
}

type createInstanceRequest struct {
	DisplayName  string  `json:"displayName"`
	DepartmentID *string `json:"departmentId,omitempty"`
}

func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}

	var req createInstanceRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	instance, err := h.provision.Execute(r.Context(), uwuc.ProvisionInput{
		WorkspaceID:  workspaceID,
		DepartmentID: req.DepartmentID,
		DisplayName:  req.DisplayName,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, toInstanceDTO(instance))
}

func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	in := uw.ListInstancesInput{
		WorkspaceID: workspaceID,
		Scope:       scope,
		Search:      query.Get("search"),
		Options:     shared.QueryOptions{Pagination: httpx.ParsePagination(query)},
	}
	if v := query.Get("status"); v != "" {
		status := uw.Status(v)
		in.Status = &status
	}

	result, err := h.list.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err)
		return
	}

	items := make([]instanceDTO, 0, len(result.Items))
	for _, instance := range result.Items {
		items = append(items, toInstanceDTO(instance))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

func (h *Handler) GetInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	instance, err := h.get.Execute(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toInstanceDTO(instance))
}

type updateInstanceRequest struct {
	DisplayName  *string `json:"displayName"`
	DepartmentID *string `json:"departmentId"`

	AgentID    *string `json:"agentId"`
	WorkflowID *string `json:"workflowId"`
	PipelineID *string `json:"pipelineId"`

	EnableAgentResponses *bool `json:"enableAgentResponses"`
	EnableWorkflow       *bool `json:"enableWorkflow"`
	EnableAnalysis       *bool `json:"enableAnalysis"`
	EnableAutoStaging    *bool `json:"enableAutoStaging"`
	EnableAutoMemory     *bool `json:"enableAutoMemory"`
	HandleGroups         *bool `json:"handleGroups"`

	DailySendCap    *int  `json:"dailySendCap"`
	SendDelayMinMS  *int  `json:"sendDelayMinMs"`
	SendDelayMaxMS  *int  `json:"sendDelayMaxMs"`
	AutoRejectCalls *bool `json:"autoRejectCalls"`
}

func (h *Handler) UpdateInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}

	var raw map[string]json.RawMessage
	if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	var req updateInstanceRequest
	if err := remarshal(raw, &req); err != nil {
		response.WriteError(w, http.StatusBadRequest, "invalid request body", nil)
		return
	}

	in := uwuc.UpdateInstanceConfigInput{
		InstanceID:  mux.Vars(r)["id"],
		WorkspaceID: workspaceID,
		Scope:       scope,

		DisplayName: req.DisplayName,

		EnableAgentResponses: req.EnableAgentResponses,
		EnableWorkflow:       req.EnableWorkflow,
		EnableAnalysis:       req.EnableAnalysis,
		EnableAutoStaging:    req.EnableAutoStaging,
		EnableAutoMemory:     req.EnableAutoMemory,
		HandleGroups:         req.HandleGroups,

		DailySendCap:    req.DailySendCap,
		SendDelayMinMS:  req.SendDelayMinMS,
		SendDelayMaxMS:  req.SendDelayMaxMS,
		AutoRejectCalls: req.AutoRejectCalls,
	}
	if _, ok := raw["departmentId"]; ok {
		in.DepartmentID = &req.DepartmentID
	}
	if _, ok := raw["agentId"]; ok {
		in.AgentID = &req.AgentID
	}
	if _, ok := raw["workflowId"]; ok {
		in.WorkflowID = &req.WorkflowID
	}
	if _, ok := raw["pipelineId"]; ok {
		in.PipelineID = &req.PipelineID
	}

	instance, err := h.updateCfg.Execute(r.Context(), in)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toInstanceDTO(instance))
}

func (h *Handler) DeleteInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	err := h.remove.Execute(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "deleted"})
}

type connectRequest struct {
	Mode       string `json:"mode"`
	Phone      string `json:"phone,omitempty"`
	SystemName string `json:"systemName,omitempty"`
}

func (h *Handler) Connect(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}

	var req connectRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	challenge, err := h.connect.Connect(r.Context(), uwuc.ConnectRequest{
		InstanceID:  mux.Vars(r)["id"],
		WorkspaceID: workspaceID,
		Scope:       scope,
		Mode:        uw.ConnectMode(req.Mode),
		Phone:       req.Phone,
		SystemName:  req.SystemName,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLinkChallengeDTO(challenge))
}

func (h *Handler) LinkStatus(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	challenge, err := h.connect.Status(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLinkChallengeDTO(challenge))
}

func (h *Handler) Disconnect(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	err := h.connect.Disconnect(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "disconnected"})
}

func (h *Handler) Reset(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	err := h.connect.Reset(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": "resetting"})
}

func (h *Handler) RotateWebhookToken(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}
	instance, err := h.rotateToken.Execute(r.Context(), mux.Vars(r)["id"], workspaceID, scope)
	if err != nil {
		writeDomainError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toInstanceDTO(instance))
}

func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, uw.ErrInstanceNotFound), errors.Is(err, uw.ErrServerNotFound),
		errors.Is(err, uw.ErrContactNotFound), errors.Is(err, uw.ErrConversationNotFound),
		errors.Is(err, uw.ErrGroupNotFound), errors.Is(err, uwuc.ErrGroupNotInWorkspace):
		response.WriteError(w, http.StatusNotFound, "not found", nil)

	case errors.Is(err, uw.ErrNotGroupAdmin):
		response.WriteError(w, http.StatusForbidden,
			"the connected number is not an admin of this group", nil)

	case errors.Is(err, uw.ErrNotAGroupJID), errors.Is(err, uw.ErrGroupNameRequired),
		errors.Is(err, uw.ErrGroupNameTooLong), errors.Is(err, uw.ErrGroupTopicTooLong),
		errors.Is(err, uw.ErrNoParticipants), errors.Is(err, uw.ErrInvalidGroupAction):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)

	case errors.Is(err, uw.ErrNoInstanceAllowance):
		response.WriteError(w, http.StatusPaymentRequired,
			"this workspace has no unofficial WhatsApp numbers included; ask your account manager to grant an allowance", nil)

	case errors.Is(err, uw.ErrInstanceLimitReached):
		response.WriteError(w, http.StatusPaymentRequired, err.Error(), nil)

	case errors.Is(err, uw.ErrEntitlementUnavailable):
		response.WriteError(w, http.StatusServiceUnavailable,
			"could not verify this workspace's number allowance; try again shortly", nil)

	case errors.Is(err, uw.ErrNoServerCapacity):
		response.WriteError(w, http.StatusServiceUnavailable,
			"no capacity to connect a new number right now; try again shortly", nil)

	case errors.Is(err, uw.ErrNumberAlreadyLinked):
		response.WriteError(w, http.StatusConflict,
			"this WhatsApp number is already connected to a workspace", nil)

	case errors.Is(err, uw.ErrRestrictedByWA):
		response.WriteError(w, http.StatusConflict,
			"WhatsApp is currently restricting this number", nil)

	case errors.Is(err, uw.ErrStatusTransition), errors.Is(err, uw.ErrInstanceNotConnected):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)

	case errors.Is(err, uw.ErrWorkspaceIDRequired), errors.Is(err, uw.ErrPhoneRequired),
		errors.Is(err, uw.ErrPhoneInvalid), errors.Is(err, uw.ErrInvalidStatus),
		errors.Is(err, uw.ErrInstanceNameRequired):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)

	default:
		if provErr, ok := uw.AsProviderError(err); ok {
			writeProviderError(w, provErr)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, err.Error(), nil)
	}
}

func writeProviderError(w http.ResponseWriter, provErr *uw.ProviderError) {
	message := provErr.LocalizedMessage
	if message == "" {
		message = provErr.Message
	}

	switch {
	case provErr.IsRestriction():
		response.WriteError(w, http.StatusConflict, message, nil)
	case provErr.AtCapacity():
		response.WriteError(w, http.StatusServiceUnavailable, message, nil)
	case provErr.NeedsReconnect():
		response.WriteError(w, http.StatusConflict,
			"this number's session is no longer valid; reconnect it", nil)
	default:
		response.WriteError(w, http.StatusBadGateway, message, nil)
	}
}

func remarshal(raw map[string]json.RawMessage, out any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

type startConversationRequest struct {
	PhoneNumber string `json:"phoneNumber"`
	Name        string `json:"name"`
}

type startedConversationDTO struct {
	ConversationID string `json:"conversationId"`
	ContactID      string `json:"contactId"`
	PhoneNumber    string `json:"phoneNumber"`
	DisplayName    string `json:"displayName"`
	EntryType      string `json:"entryType"`
	AlreadyExisted bool   `json:"alreadyExisted"`
}

func (h *Handler) StartConversation(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}

	var req startConversationRequest
	if r.Body != nil {
		_ = json.NewDecoder(r.Body).Decode(&req)
	}

	started, err := h.startConv.Execute(r.Context(), uwuc.StartConversationInput{
		WorkspaceID: workspaceID,
		Scope:       scope,
		InstanceID:  mux.Vars(r)["id"],
		PhoneNumber: req.PhoneNumber,
		Name:        req.Name,
	})
	if err != nil {
		writeDomainError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, startedConversationDTO{
		ConversationID: started.ConversationID,
		ContactID:      started.ContactID,
		PhoneNumber:    started.PhoneNumber,
		DisplayName:    started.DisplayName,
		EntryType:      string(shared.EntryTypeUnofficialWhatsApp),
		AlreadyExisted: started.AlreadyExisted,
	})
}
