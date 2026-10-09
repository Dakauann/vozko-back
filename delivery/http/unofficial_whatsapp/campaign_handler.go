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

type CampaignHandler struct {
	create    uwc.CreateCampaignUseCase
	update    uwc.UpdateCampaignUseCase
	get       uwc.GetCampaignUseCase
	list      uwc.ListCampaignsUseCase
	remove    uwc.DeleteCampaignUseCase
	assignDep uwc.AssignDepartmentUseCase
	summary   uwc.GetSummaryUseCase
	entries   uwc.ListEntriesUseCase
	reset     uwc.ResetCampaignUseCase
	clear     uwc.ClearHistoryUseCase
	addEntry  uwc.AddEntriesUseCase
	updEntry  uwc.UpdateEntryUseCase
	delEntry  uwc.DeleteEntryUseCase
	quickSend uwc.QuickSendUseCase
	validate  uwc.ValidateTargetsUseCase

	departments DepartmentScopeResolver
	access      uwc.CampaignAccessUseCase
	actions     uwc.CampaignActionUseCase
}

type CampaignHandlerDeps struct {
	Create      uwc.CreateCampaignUseCase
	Update      uwc.UpdateCampaignUseCase
	Get         uwc.GetCampaignUseCase
	Access      uwc.CampaignAccessUseCase
	Actions     uwc.CampaignActionUseCase
	List        uwc.ListCampaignsUseCase
	Delete      uwc.DeleteCampaignUseCase
	AssignDep   uwc.AssignDepartmentUseCase
	Summary     uwc.GetSummaryUseCase
	Entries     uwc.ListEntriesUseCase
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
		entries: d.Entries, reset: d.Reset, clear: d.Clear,
		addEntry: d.AddEntry, updEntry: d.UpdateEntry, delEntry: d.DeleteEntry,
		quickSend: d.QuickSend, validate: d.Validate, departments: d.Departments, access: d.Access, actions: d.Actions,
	}
}

func (h *CampaignHandler) campaignScope(w http.ResponseWriter, r *http.Request) (string, uw.DepartmentScope, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusForbidden, "workspace is required", nil)
		return "", uw.DepartmentScope{}, false
	}
	if h.departments == nil {
		response.WriteError(w, http.StatusForbidden, "you do not have access to this workspace's campaigns", nil)
		return "", uw.DepartmentScope{}, false
	}
	claims := middleware.GetClaims(r)
	if claims == nil {
		response.WriteError(w, http.StatusUnauthorized, "Unauthorized", nil)
		return "", uw.DepartmentScope{}, false
	}
	scope, allowed := uw.ResolveScope(h.departments, claims.UserID, workspaceID, claims.Role == "admin")
	if !allowed {
		response.WriteError(w, http.StatusForbidden,
			"you do not have access to this workspace's campaigns", nil)
		return "", uw.DepartmentScope{}, false
	}
	return workspaceID, scope, true
}

func (h *CampaignHandler) ownedCampaign(w http.ResponseWriter, r *http.Request) (string, uw.DepartmentScope, *uwc.Campaign, bool) {
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return "", uw.DepartmentScope{}, nil, false
	}
	if h.access == nil {
		response.WriteError(w, http.StatusNotFound, uwc.ErrCampaignNotFound.Error(), nil)
		return "", uw.DepartmentScope{}, nil, false
	}
	c, err := h.access.Owned(r.Context(), workspaceID, scope, mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return "", uw.DepartmentScope{}, nil, false
	}
	return workspaceID, scope, c, true
}

func (h *CampaignHandler) List(w http.ResponseWriter, r *http.Request) {
	workspaceID, _, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
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

var campaignSortFields = map[string]string{
	"name":      "name",
	"status":    "status",
	"createdAt": "createdAt",
	"updatedAt": "updatedAt",
}

func (h *CampaignHandler) ListArchived(w http.ResponseWriter, r *http.Request) {
	q := r.URL.Query()
	q.Set("archived", "true")
	r.URL.RawQuery = q.Encode()
	h.List(w, r)
}

func (h *CampaignHandler) Get(w http.ResponseWriter, r *http.Request) {
	_, _, c, ok := h.ownedCampaign(w, r)
	if !ok {
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
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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

// @Summary		Editar uma campanha não oficial
// @Description	Atualiza mensagem, número, ritmo, fluxo e agente de uma campanha que não está em andamento. Uma campanha preparada a partir de leads (origem `lead_selection`) recusa a troca do modelo, do número ou da mensagem com 409 `send_selection_locked`. O fluxo e o agente são conferidos pelo passo compartilhado: fluxo de outro workspace responde 403 `campaign_workflow_forbidden`, fluxo ou agente inexistente 422, variável do agente ausente 400 `AGENT_REQUIRED_VARIABLE_MISSING`, passo indisponível 503.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			body	body		object	true	"Campos da campanha ou números a incluir"
// @Success		200		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id} [put]
func (h *CampaignHandler) Update(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, _, ok := h.ownedCampaign(w, r)
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
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
		return
	}
	if err := h.remove.Execute(mux.Vars(r)["id"]); err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]bool{"deleted": true})
}

func (h *CampaignHandler) AssignDepartment(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
		return
	}
	updated, err := h.assignDep.Execute(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(updated))
}

func (h *CampaignHandler) Archive(w http.ResponseWriter, r *http.Request) { h.setArchived(w, r, true) }
func (h *CampaignHandler) Unarchive(w http.ResponseWriter, r *http.Request) {
	h.setArchived(w, r, false)
}

func (h *CampaignHandler) setArchived(w http.ResponseWriter, r *http.Request, archived bool) {
	workspaceID, scope, existing, ok := h.ownedCampaign(w, r)
	if !ok {
		return
	}
	id := existing.ID
	existing.Archived = archived
	existing.WorkspaceID = workspaceID

	updated, err := h.update.Execute(r.Context(), id, existing, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(updated))
}

// @Summary		Iniciar uma campanha não oficial
// @Description	Uma campanha preparada a partir de leads (origem `lead_selection`), parada, só começa pela revisão (POST /leads/actions/sends/start), que pula quem entrou em outra campanha em andamento e confere o limite diário: por aqui responde 409 `send_start_from_leads`. Pausada, ela retoma por aqui. O mesmo vale para o início agendado e para as ferramentas do assistente.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Success		200		{object}	map[string]interface{}
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/start [post]
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
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	if h.actions == nil {
		response.WriteError(w, http.StatusNotFound, uwc.ErrCampaignNotFound.Error(), nil)
		return
	}
	if _, err := h.actions.Act(r.Context(), workspaceID, scope, mux.Vars(r)["id"], action); err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, map[string]string{"status": string(action)})
}

// @Summary		Envio rápido de uma campanha não oficial
// @Description	Inclui os números enviados (opcional) e dispara os pendentes. Uma campanha preparada a partir de leads nunca passa pelo envio rápido, com ou sem números: responde 409 `send_selection_locked`; use POST /leads/actions/sends/start.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			body	body		object	true	"Campos da campanha ou números a incluir"
// @Success		200		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/quick-send [post]
func (h *CampaignHandler) QuickSend(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
		return
	}
	var payload struct {
		Numbers []campaignTargetDTO `json:"numbers"`
	}
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

func (h *CampaignHandler) Validate(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
		return
	}
	out, err := h.validate.Execute(r.Context(), mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Preparar o reinício de uma campanha não oficial
// @Description	Gera o código que confirma o reinício. Uma campanha preparada a partir de leads (origem `lead_selection`) não reinicia: responde 409 `send_selection_locked`; prepare um novo envio a partir dos leads.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Success		200		{object}	map[string]interface{}
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/reset/prepare [post]
func (h *CampaignHandler) PrepareReset(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
		return
	}
	out, err := h.reset.PrepareReset(mux.Vars(r)["id"])
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, out)
}

// @Summary		Reiniciar uma campanha não oficial
// @Description	Volta todos os números para PENDING com o código de `reset/prepare`. Uma campanha preparada a partir de leads (origem `lead_selection`) não reinicia: responde 409 `send_selection_locked`, porque os leads pulados na revisão (código 9200xx) seriam enviados.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			body	body		object	true	"{resetCode}"
// @Success		200		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/reset [post]
func (h *CampaignHandler) ConfirmReset(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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

// @Summary		Incluir números numa campanha não oficial
// @Description	Inclui números numa campanha que não está em andamento. Uma campanha preparada a partir de leads não recebe números: responde 409 `send_selection_locked`. O fluxo e o agente da campanha são conferidos pelo passo compartilhado.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			body	body		object	true	"Campos da campanha ou números a incluir"
// @Success		201		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/entries [post]
func (h *CampaignHandler) AddEntries(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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

// @Summary		Editar um número de uma campanha não oficial
// @Description	Troca número, nome, variáveis ou metadados de um número numa campanha que não está em andamento. Uma campanha preparada a partir de leads (origem `lead_selection`) não aceita a edição: responde 409 `send_selection_locked`.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			id		path		string	true	"Campanha"
// @Param			entryId	path		string	true	"Número da campanha"
// @Param			body	body		object	true	"{number?, name?, variables?, metadata?}"
// @Success		200		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		409		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/entries/{entryId} [patch]
func (h *CampaignHandler) UpdateEntry(w http.ResponseWriter, r *http.Request) {
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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
	if _, _, _, ok := h.ownedCampaign(w, r); !ok {
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

func writeCampaignError(w http.ResponseWriter, err error) {
	if httpx.WriteSelectionSendRefusal(w, err) {
		return
	}
	var unusable *uwc.InstanceUnusableError
	switch {
	case errors.Is(err, uwc.ErrCampaignNotFound), errors.Is(err, uwc.ErrEntryNotFound):
		response.WriteError(w, http.StatusNotFound, err.Error(), nil)

	case errors.Is(err, uw.ErrInstanceOutsideDepartment), errors.Is(err, uw.ErrInstanceNotFound):
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

	case errors.Is(err, campaign.ErrWorkflowForbidden):
		response.WriteErrorWithCode(w, http.StatusForbidden, campaign.ErrorCode(err), err.Error(), nil)

	case errors.Is(err, campaign.ErrAgentVarsMissing):
		response.WriteErrorWithCode(w, http.StatusBadRequest, campaign.ErrorCode(err), err.Error(), nil)

	case errors.Is(err, campaign.ErrWorkflowNotFound),
		errors.Is(err, campaign.ErrAgentNotFound):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, campaign.ErrorCode(err), err.Error(), nil)

	case errors.Is(err, campaign.ErrAutomationUnavailable),
		errors.Is(err, campaign.ErrIdempotencyUnavailable),
		errors.Is(err, campaign.ErrLeadTargetsUnavailable):
		response.WriteError(w, http.StatusServiceUnavailable, err.Error(), nil)

	default:
		response.WriteError(w, http.StatusInternalServerError, "campaign request failed", nil)
	}
}
