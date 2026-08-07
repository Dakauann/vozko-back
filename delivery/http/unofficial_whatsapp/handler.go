// Package unofficial_whatsapp serves the unofficial WhatsApp channel's HTTP
// surface.
//
// It decodes, enforces that a workspace is present, delegates, and maps domain
// errors onto status codes. Every rule about what is allowed lives below this
// layer, so a second caller — the cron, a workflow — cannot get a different
// answer than an HTTP client.
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

// Handler serves the connected-number endpoints.
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
	// departments answers which departments the caller may act within. Optional:
	// a nil resolver means UNRESTRICTED, which is only correct in a test — the
	// container logs the capability at boot so an unwired one is visible there
	// rather than discovered as one department reading another's numbers.
	departments DepartmentScopeResolver
}

// DepartmentScopeResolver reports the caller's department scope.
//
// A narrow port satisfied by the platform's conversation authorizer, which
// already owns this question for every channel: membership, role and the
// conversations:read permission all feed it. Re-deriving any of that here would
// be a second implementation of an access rule, and the two would diverge the
// first time the platform's changed.
type DepartmentScopeResolver interface {
	GetDepartmentScope(userID, workspaceID string, isAdmin bool) (conversation.DepartmentAccessScope, bool)
}

// HandlerDeps groups the usecases.
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

// scopeFor resolves which departments this request may act within.
//
// Returns false when the caller may not be here at all — not a member, or
// without the conversations:read permission the platform requires to see any
// conversation. The handler answers 403 in that case rather than an empty list,
// because "you have no departments" and "you may not ask" are different answers.
//
// An absent resolver yields UNRESTRICTED. That is the pre-existing behaviour and
// it fails OPEN, which is why the container asserts the wiring at boot: a silent
// nil here would quietly undo every check below.
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

// requireScope resolves the scope or writes the refusal. The pair every
// instance endpoint opens with, so none of them can forget the second half.
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

// allowanceDTO is what the connect screen needs to decide what to offer.
//
// `canConnect` and `remaining` are derived SERVER-side from the same value the
// provisioning gate enforces. Deriving them in the browser would be one more
// place for the rule to live, and the first time the two disagreed an operator
// would be shown a button that cannot work.
type allowanceDTO struct {
	Limit int `json:"limit"`
	Used  int `json:"used"`
	// The two halves of Limit, so the meter can say "2 included + 3 purchased"
	// rather than only "5" — one is a decision we made, the other is something
	// the workspace bought, and they have different remedies when the meter fills.
	Granted    int  `json:"granted"`
	Purchased  int  `json:"purchased"`
	Remaining  int  `json:"remaining"`
	CanConnect bool `json:"canConnect"`
	// OverLimit is reachable without anyone doing anything wrong — an addon
	// lapses, or an administrator lowers a grant. Surfaced so the UI can explain
	// it rather than silently disabling a button.
	OverLimit bool `json:"overLimit"`
}

// GetAllowance reports the workspace's number allowance and current usage.
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

// ---------------------------------------------------------------- instances

type createInstanceRequest struct {
	DisplayName  string  `json:"displayName"`
	DepartmentID *string `json:"departmentId,omitempty"`
}

// CreateInstance provisions a number slot, ready to be linked.
//
// It deliberately does NOT start the linking attempt: provisioning talks to a
// host with an admin credential and can fail on capacity, while linking is a
// user-paced flow the customer drives from their phone. Collapsing them would
// mean a capacity failure and an expired QR code arrive through the same call
// and read the same way.
func (h *Handler) CreateInstance(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return
	}

	var req createInstanceRequest
	// An empty body is valid here: every field is optional.
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

// ListInstances returns the workspace's numbers.
func (h *Handler) ListInstances(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.requireScope(w, r)
	if !ok {
		return
	}

	query := r.URL.Query()
	in := uw.ListInstancesInput{
		WorkspaceID: workspaceID,
		// The caller sees only their own departments' numbers. An owner or an
		// admin is unrestricted; a member in no department sees none.
		Scope:   scope,
		Search:  query.Get("search"),
		Options: shared.QueryOptions{Pagination: httpx.ParsePagination(query)},
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
	// WritePaginated, not WriteSuccess: every paginated list in this API answers
	// {"data": [...], "meta": {...}} and the browser client reads exactly that.
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalItems: result.TotalItems,
		TotalPages: result.TotalPages,
	})
}

// GetInstance returns one number.
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
	HandleGroups         *bool `json:"handleGroups"`

	DailySendCap    *int  `json:"dailySendCap"`
	SendDelayMinMS  *int  `json:"sendDelayMinMs"`
	SendDelayMaxMS  *int  `json:"sendDelayMaxMs"`
	AutoRejectCalls *bool `json:"autoRejectCalls"`
}

// UpdateInstance edits automation and pacing settings.
//
// The request uses raw JSON presence to distinguish "not supplied" from
// "cleared", which is why the nullable ids are re-wrapped below: a PATCH that
// omitted agentId must leave it alone, while one that sent null must clear it.
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
		HandleGroups:         req.HandleGroups,

		DailySendCap:    req.DailySendCap,
		SendDelayMinMS:  req.SendDelayMinMS,
		SendDelayMaxMS:  req.SendDelayMaxMS,
		AutoRejectCalls: req.AutoRejectCalls,
	}
	// Present-in-body decides whether a nullable field is touched at all.
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

// DeleteInstance removes a number from the workspace.
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

// ---------------------------------------------------------------- linking

type connectRequest struct {
	// Mode is "qr" or "pairing". Anything else falls back to QR, which needs no
	// extra input and therefore cannot fail on a missing field.
	Mode string `json:"mode"`
	// Phone is required for pairing mode.
	Phone      string `json:"phone,omitempty"`
	SystemName string `json:"systemName,omitempty"`
}

// Connect starts a linking attempt and returns the code to act on.
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

// LinkStatus polls the host. The connect screen calls this on a timer because
// the code rotates and the provider hands out the current one here rather than
// pushing it.
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

// Disconnect ends the session without removing the instance, so the same slot
// can be relinked to a different number.
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

// Reset restarts a wedged runtime on the host. The host enforces a cooldown and
// refuses inside it; that refusal reaches the client as a 409, not a 500,
// because it is a normal answer.
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

// RotateWebhookToken mints a new delivery URL and re-registers it.
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

// ---------------------------------------------------------------- errors

// writeDomainError maps a domain error onto a status code.
//
// The provider's own refusals are translated rather than forwarded: a host at
// capacity is a 503 the customer should retry, and a WhatsApp restriction is a
// 409 they must wait out — surfacing either as a 500 would send them to support
// for something the screen can explain.
func writeDomainError(w http.ResponseWriter, err error) {
	switch {
	case errors.Is(err, uw.ErrInstanceNotFound), errors.Is(err, uw.ErrServerNotFound),
		errors.Is(err, uw.ErrContactNotFound), errors.Is(err, uw.ErrConversationNotFound),
		errors.Is(err, uw.ErrGroupNotFound), errors.Is(err, uwuc.ErrGroupNotInWorkspace):
		response.WriteError(w, http.StatusNotFound, "not found", nil)

	// 403, not 400: the request is well-formed and the group exists, the
	// connected number simply is not an admin of it. The UI already hides these
	// controls, so reaching this means state changed under the operator, and
	// "you are not an admin of this group" is the only useful thing to say.
	case errors.Is(err, uw.ErrNotGroupAdmin):
		response.WriteError(w, http.StatusForbidden,
			"the connected number is not an admin of this group", nil)

	case errors.Is(err, uw.ErrNotAGroupJID), errors.Is(err, uw.ErrGroupNameRequired),
		errors.Is(err, uw.ErrGroupNameTooLong), errors.Is(err, uw.ErrGroupTopicTooLong),
		errors.Is(err, uw.ErrNoParticipants), errors.Is(err, uw.ErrInvalidGroupAction):
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)

	// 402, not 403: the caller has permission, they have run out of allowance.
	// The distinction is what lets the UI offer an upsell instead of "contact
	// your administrator", and the two errors are kept apart because their
	// remedies are — one needs a grant, the other needs a purchase.
	case errors.Is(err, uw.ErrNoInstanceAllowance):
		response.WriteError(w, http.StatusPaymentRequired,
			"this workspace has no unofficial WhatsApp numbers included; ask your account manager to grant an allowance", nil)

	case errors.Is(err, uw.ErrInstanceLimitReached):
		response.WriteError(w, http.StatusPaymentRequired, err.Error(), nil)

	// 503, not 500: the request was fine and retrying may work. Provisioning
	// fails closed on an unreadable entitlement rather than assuming unlimited.
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

// writeProviderError surfaces a provider failure the caller can act on.
func writeProviderError(w http.ResponseWriter, provErr *uw.ProviderError) {
	// The provider's own pt-BR wording describes a state the customer can
	// verify in their WhatsApp; rewording it would drift from what they see.
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

// remarshal re-decodes a raw field map into a typed struct, so presence and
// value can both be read from one body without decoding the request twice.
func remarshal(raw map[string]json.RawMessage, out any) error {
	encoded, err := json.Marshal(raw)
	if err != nil {
		return err
	}
	return json.Unmarshal(encoded, out)
}

// startConversationRequest is an operator reaching a number by hand.
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

// StartConversation opens a conversation with a number that never wrote to us.
//
// Gated by ActionSend rather than ActionUpdate: replying is attendance, but
// messaging a stranger is cold outbound, which is what gets an unofficial number
// banned. The route enforces that; this handler only translates.
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
		// Returned so the CRM can route straight to the inbox without having to
		// know this channel's entry type by heart.
		EntryType:      string(shared.EntryTypeUnofficialWhatsApp),
		AlreadyExisted: started.AlreadyExisted,
	})
}
