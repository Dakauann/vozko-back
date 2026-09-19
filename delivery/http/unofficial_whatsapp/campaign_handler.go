package unofficial_whatsapp

import (
	"encoding/json"
	"errors"
	"net/http"
	"strings"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	"vozko/domain/campaign"
	"vozko/domain/shared"
	uw "vozko/domain/unofficial_whatsapp"
	uwc "vozko/domain/unofficial_whatsapp_campaign"
	"vozko/domain/user"
	"vozko/infra/http/middleware"
	uwcuc "vozko/usecases/unofficial_whatsapp_campaign"
)

// CampaignHandler serves the unofficial WhatsApp campaign endpoints.
//
// Decode, enforce the workspace and the department scope, delegate, map errors.
// Every rule about what is allowed lives below this layer, so the cron and a
// workflow get the same answers an HTTP client does.
type CampaignHandler struct {
	create    uwc.CreateCampaignUseCase
	update    uwc.UpdateCampaignUseCase
	get       uwc.GetCampaignUseCase
	list      uwc.ListCampaignsUseCase
	remove    uwc.DeleteCampaignUseCase
	assignDep uwc.AssignDepartmentUseCase
	summary   uwc.GetSummaryUseCase
	entries   uwc.ListEntriesUseCase
	dispatch  uwc.DispatchCampaignUseCase
	reset     uwc.ResetCampaignUseCase
	clear     uwc.ClearHistoryUseCase
	addEntry  uwc.AddEntriesUseCase
	updEntry  uwc.UpdateEntryUseCase
	delEntry  uwc.DeleteEntryUseCase
	quickSend uwc.QuickSendUseCase
	validate  uwc.ValidateTargetsUseCase

	departments DepartmentScopeResolver
}

// CampaignHandlerDeps groups the usecases.
type CampaignHandlerDeps struct {
	Create      uwc.CreateCampaignUseCase
	Update      uwc.UpdateCampaignUseCase
	Get         uwc.GetCampaignUseCase
	List        uwc.ListCampaignsUseCase
	Delete      uwc.DeleteCampaignUseCase
	AssignDep   uwc.AssignDepartmentUseCase
	Summary     uwc.GetSummaryUseCase
	Entries     uwc.ListEntriesUseCase
	Dispatch    uwc.DispatchCampaignUseCase
	Reset       uwc.ResetCampaignUseCase
	Clear       uwc.ClearHistoryUseCase
	AddEntry    uwc.AddEntriesUseCase
	UpdateEntry uwc.UpdateEntryUseCase
	DeleteEntry uwc.DeleteEntryUseCase
	QuickSend   uwc.QuickSendUseCase
	Validate    uwc.ValidateTargetsUseCase
	Departments DepartmentScopeResolver
}

func NewCampaignHandler(d CampaignHandlerDeps) *CampaignHandler {
	return &CampaignHandler{
		create: d.Create, update: d.Update, get: d.Get, list: d.List,
		remove: d.Delete, assignDep: d.AssignDep, summary: d.Summary,
		entries: d.Entries, dispatch: d.Dispatch, reset: d.Reset, clear: d.Clear,
		addEntry: d.AddEntry, updEntry: d.UpdateEntry, delEntry: d.DeleteEntry,
		quickSend: d.QuickSend, validate: d.Validate, departments: d.Departments,
	}
}

// campaignScope resolves the caller's department scope, mirroring the instance
// handler's helper so both surfaces answer access the same way.
func (h *CampaignHandler) campaignScope(w http.ResponseWriter, r *http.Request) (string, uw.DepartmentScope, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", uw.DepartmentScope{}, false
	}
	if h.departments == nil {
		return workspaceID, uw.Unrestricted(), true
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return "", uw.DepartmentScope{}, false
	}
	scope, allowed := h.departments.GetDepartmentScope(
		claims.UserID, workspaceID, claims.Role == "admin")
	if !allowed {
		response.WriteError(w, http.StatusForbidden,
			"you do not have access to this workspace's campaigns", nil)
		return "", uw.DepartmentScope{}, false
	}
	return workspaceID, uw.DepartmentScope{
		DepartmentIDs: scope.DepartmentIDs,
		Restrict:      scope.Restrict,
	}, true
}

// ---------------------------------------------------------------- read

func (h *CampaignHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	// A caller scoped to no department sees nothing, which the shared helper
	// answers for every channel rather than each one re-deriving it.
	if httpx.ShouldReturnEmptyDepartmentList(r) {
		response.WritePaginated(w, http.StatusOK, []campaignDTO{}, response.PaginationMeta{})
		return
	}

	values := r.URL.Query()
	archived := httpx.ParseBoolQuery(values.Get("archived"))
	input := uwc.ListCampaignsInput{
		WorkspaceID:   workspaceID,
		DepartmentIDs: httpx.DepartmentFilterIDs(r),
		InstanceIDs:   httpx.ParseCSVQuery(values["instanceId"]),
		Search:        strings.TrimSpace(values.Get("search")),
		Status:        campaign.Status(strings.ToUpper(strings.TrimSpace(values.Get("status")))),
		Archived:      archived,
		Options: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
			Sorts:      httpx.ParseSort(values, campaignSortFields),
		},
	}

	result, err := h.list.Execute(r.Context(), input)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "failed to list campaigns", nil)
		return
	}

	items := make([]campaignDTO, 0, len(result.Items))
	for _, c := range result.Items {
		items = append(items, campaignToDTO(c))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// campaignSortFields is an allowlist. A sort key is caller-supplied and
// interpolating one into ORDER BY is an injection.
var campaignSortFields = map[string]string{
	"name":      "name",
	"status":    "status",
	"createdAt": "createdAt",
	"updatedAt": "updatedAt",
}

// ListArchived is the archived view. A separate endpoint rather than a query
// flag so the two have distinct permissions and distinct URLs to link to,
// matching the official channel.
func (h *CampaignHandler) ListArchived(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("archived", "true")
	r.URL.RawQuery = q.Encode()
	h.List(w, r)
}

func (h *CampaignHandler) Get(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	c, err := h.get.Execute(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(c))
}

func (h *CampaignHandler) Summary(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	if httpx.ShouldReturnEmptyDepartmentList(r) {
		response.WriteSuccess(w, http.StatusOK, campaign.NewMetrics(nil))
		return
	}

	values := r.URL.Query()
	metrics, err := h.summary.Execute(uwc.WorkspaceSummaryFilter{
		WorkspaceID:   workspaceID,
		DepartmentIDs: httpx.DepartmentFilterIDs(r),
		InstanceIDs:   httpx.ParseCSVQuery(values["instanceId"]),
		CreatedFrom:   httpx.ParseDateBound(values.Get("from"), false),
		CreatedTo:     httpx.ParseDateBound(values.Get("to"), true),
	})
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "failed to load the campaigns summary", nil)
		return
	}
	response.WriteSuccess(w, http.StatusOK, metrics)
}

func (h *CampaignHandler) ListEntries(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	values := r.URL.Query()
	errorCode := 0
	if v := httpx.ParseIntQuery(values.Get("errorCode")); v != nil {
		errorCode = *v
	}

	result, err := h.entries.Execute(uwc.ListEntriesInput{
		CampaignID: mux.Vars(r)["id"],
		Status:     campaign.SendStatus(strings.ToUpper(strings.TrimSpace(values.Get("status")))),
		Number:     strings.TrimSpace(values.Get("number")),
		Search:     strings.TrimSpace(values.Get("search")),
		ErrorCode:  errorCode,
		Options: shared.QueryOptions{
			Pagination: httpx.ParsePagination(values),
		},
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}

	items := make([]campaignEntryDTO, 0, len(result.Items))
	for _, e := range result.Items {
		items = append(items, entryToDTO(e))
	}
	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// ---------------------------------------------------------------- write

func (h *CampaignHandler) Create(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	var payload campaignPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}

	draft := payload.toDomain(workspaceID)
	// Pre-settled results are a PLATFORM privilege, and a different question
	// from the one the route already answered: that gate asks who may launch a
	// campaign, this asks who may create one that claims to have already run.
	// A workspace owner passes the first and not the second.
	//
	// Dropped rather than refused, the same bargain the lead import makes with
	// its scripted seeding: a caller who cannot use the control has simply asked
	// for an ordinary campaign, and they get one.
	if claims := middleware.GetClaims(r); claims == nil || claims.Role != string(user.RoleAdmin) {
		draft.SeedOutcome = nil
	}

	created, err := h.create.Execute(r.Context(), draft, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, campaignToDTO(created))
}

func (h *CampaignHandler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	var payload campaignPayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}

	updated, err := h.update.Execute(r.Context(), mux.Vars(r)["id"], payload.toDomain(workspaceID), scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(updated))
}

func (h *CampaignHandler) Delete(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	if err := h.remove.Execute(mux.Vars(r)["id"]); err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *CampaignHandler) AssignDepartment(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	updated, err := h.assignDep.Execute(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(updated))
}

// Archive and Unarchive flip the flag through the update use case, so archiving
// goes through the same validation and the same persistence as any other edit.
func (h *CampaignHandler) Archive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, true) }
func (h *CampaignHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, false)
}

func (h *CampaignHandler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	id := mux.Vars(r)["id"]

	existing, err := h.get.Execute(r.Context(), id)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	existing.Archived = archived
	existing.WorkspaceID = workspaceID

	updated, err := h.update.Execute(r.Context(), id, existing, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(updated))
}

// ---------------------------------------------------------------- lifecycle

func (h *CampaignHandler) Start(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, campaign.ActionStart)
}
func (h *CampaignHandler) Pause(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, campaign.ActionPause)
}
func (h *CampaignHandler) Stop(w http.ResponseWriter, r *http.Request) {
	h.act(w, r, campaign.ActionStop)
}

func (h *CampaignHandler) act(w http.ResponseWriter, r *http.Request, action campaign.Action) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	err := h.dispatch.Dispatch(r.Context(), uwc.DispatchCampaignInput{
		CampaignID: mux.Vars(r)["id"],
		Action:     action,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": string(action)})
}

func (h *CampaignHandler) QuickSend(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	var payload struct {
		Numbers []campaignTargetDTO `json:"numbers"`
	}
	// An empty body is a valid quick send: it means "dispatch what is already
	// pending", which is what the detail screen's Send button does.
	_ = json.NewDecoder(r.Body).Decode(&payload)

	numbers := make([]uwc.EntryInput, 0, len(payload.Numbers))
	for _, n := range payload.Numbers {
		numbers = append(numbers, uwc.EntryInput{
			Number: n.Number, Name: n.Name, Variables: n.Variables, Metadata: n.Metadata,
		})
	}

	out, err := h.quickSend.Execute(r.Context(), uwc.QuickSendInput{
		CampaignID: mux.Vars(r)["id"],
		Numbers:    numbers,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// Validate runs the optional up-front list clean.
func (h *CampaignHandler) Validate(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	out, err := h.validate.Execute(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- reset / clear

func (h *CampaignHandler) PrepareReset(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	out, err := h.reset.PrepareReset(mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *CampaignHandler) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	var payload struct {
		ResetCode string `json:"resetCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"resetCode": "string"})
		return
	}
	out, err := h.reset.ConfirmReset(uwc.ResetCampaignInput{
		CampaignID: mux.Vars(r)["id"], ResetCode: payload.ResetCode,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *CampaignHandler) PrepareClearHistory(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	out, err := h.clear.PrepareClearHistory(mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *CampaignHandler) ConfirmClearHistory(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	var payload struct {
		ClearCode string `json:"clearCode"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"clearCode": "string"})
		return
	}
	out, err := h.clear.ConfirmClearHistory(uwc.ClearHistoryInput{
		CampaignID: mux.Vars(r)["id"], ClearCode: payload.ClearCode,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// ---------------------------------------------------------------- entries

func (h *CampaignHandler) AddEntries(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	var payload struct {
		Numbers []campaignTargetDTO `json:"numbers"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{"numbers": "array"})
		return
	}

	numbers := make([]uwc.EntryInput, 0, len(payload.Numbers))
	for _, n := range payload.Numbers {
		numbers = append(numbers, uwc.EntryInput{
			Number: n.Number, Name: n.Name, Variables: n.Variables, Metadata: n.Metadata,
		})
	}

	out, err := h.addEntry.Execute(r.Context(), uwc.AddEntriesInput{
		CampaignID: mux.Vars(r)["id"], Numbers: numbers,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *CampaignHandler) UpdateEntry(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	var payload struct {
		Number    *string                `json:"number,omitempty"`
		Name      *string                `json:"name,omitempty"`
		Variables []string               `json:"variables,omitempty"`
		Metadata  map[string]interface{} `json:"metadata,omitempty"`
	}
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}

	vars := mux.Vars(r)
	out, err := h.updEntry.Execute(r.Context(), uwc.UpdateEntryInput{
		CampaignID: vars["id"], EntryID: vars["entryId"],
		Number: payload.Number, Name: payload.Name,
		Variables: payload.Variables, Metadata: payload.Metadata,
	})
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

func (h *CampaignHandler) DeleteEntry(w http.ResponseWriter, r *http.Request) {
	if _, _, ok := h.campaignScope(w, r); !ok {
		return
	}
	vars := mux.Vars(r)
	if err := h.delEntry.Execute(uwc.DeleteEntryInput{
		CampaignID: vars["id"], EntryID: vars["entryId"],
	}); err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

// ---------------------------------------------------------------- errors

// writeCampaignError maps domain refusals onto status codes.
//
// One table so every endpoint answers the same refusal the same way. The
// lifecycle errors come from the SHARED kernel, which is why "already running"
// reads identically on both channels.
func writeCampaignError(w http.ResponseWriter, err error) {
	var unusable *uwc.InstanceUnusableError
	switch {
	case errors.Is(err, uwc.ErrCampaignNotFound), errors.Is(err, uwc.ErrEntryNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)

	case errors.Is(err, uw.ErrInstanceOutsideDepartment), errors.Is(err, uw.ErrInstanceNotFound):
		// Deliberately indistinguishable from "no such number": whether a number
		// exists in a department you are not in is itself information.
		response.WriteError(w, http.StatusNotFound, "number not found", nil)

	case errors.Is(err, campaign.ErrAlreadyRunning),
		errors.Is(err, campaign.ErrNotRunning),
		errors.Is(err, campaign.ErrAlreadyStopped),
		errors.Is(err, uwc.ErrCampaignRunning),
		errors.Is(err, uwc.ErrCampaignResetNotAllowed),
		errors.Is(err, uwc.ErrCampaignClearNotAllowed),
		errors.Is(err, uwcuc.ErrQuickSendBusy):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)

	case errors.Is(err, uwc.ErrCampaignResetCodeInvalid), errors.Is(err, uwc.ErrCampaignClearCodeInvalid):
		response.WriteError(w, http.StatusForbidden, err.Error(), nil)

	case errors.As(err, &unusable),
		errors.Is(err, uw.ErrRestrictedByWA),
		errors.Is(err, uw.ErrInstanceNotConnected):
		// 422: the request is well formed, the number is simply not in a state
		// that can run a campaign. The message carries the remedy.
		response.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)

	case errors.Is(err, uwc.ErrCampaignNameRequired),
		errors.Is(err, uwc.ErrCampaignInstanceIDRequired),
		errors.Is(err, uwc.ErrCampaignTargetsRequired),
		errors.Is(err, uwc.ErrCampaignTargetsTooMany),
		errors.Is(err, uwc.ErrCampaignTargetInvalid),
		errors.Is(err, uwc.ErrCampaignVariablesMismatch),
		errors.Is(err, uwc.ErrCampaignVariableEmpty),
		errors.Is(err, uwc.ErrCampaignWorkflowVarsMissing),
		errors.Is(err, uwc.ErrCampaignScheduledStartInvalid),
		errors.Is(err, uwc.ErrCampaignScheduledStartTooSoon),
		errors.Is(err, uwc.ErrMessageKindInvalid),
		errors.Is(err, uwc.ErrMessageBodyRequired),
		errors.Is(err, uwc.ErrMessageBodyEmpty),
		errors.Is(err, uwc.ErrMessageBodyTooLong),
		errors.Is(err, uwc.ErrMessageMediaRequired),
		errors.Is(err, uwc.ErrMessageVariantMismatch),
		errors.Is(err, uwc.ErrMenuOptionsRequired),
		errors.Is(err, uwc.ErrMenuOptionsTooMany),
		errors.Is(err, uwc.ErrMenuOptionLabelTooLong),
		errors.Is(err, uwc.ErrMenuOptionIDRequired),
		errors.Is(err, campaign.ErrSeededOutcomeOverflow):
		response.WriteError(w, http.StatusUnprocessableEntity, err.Error(), nil)

	default:
		response.WriteError(w, http.StatusInternalServerError, "campaign request failed", nil)
	}
}
