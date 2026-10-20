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
	workspace_domain "vozko/domain/workspace"
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
	estimate  uwc.EstimateCampaignUseCase

	departments DepartmentScopeResolver
	access      uwc.CampaignAccessUseCase
	actions     uwc.CampaignActionUseCase
	permissions workspace_domain.CheckAccessUseCase
	progress    campaign.ProgressUseCase
	timeline    campaign.TimelineUseCase
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
	Estimate    uwc.EstimateCampaignUseCase
	Departments DepartmentScopeResolver
	Permissions workspace_domain.CheckAccessUseCase
}

func NewCampaignHandler(d CampaignHandlerDeps) *CampaignHandler {
	return &CampaignHandler{
		create: d.Create, update: d.Update, get: d.Get, list: d.List,
		remove: d.Delete, assignDep: d.AssignDep, summary: d.Summary,
		entries: d.Entries, reset: d.Reset, clear: d.Clear,
		addEntry: d.AddEntry, updEntry: d.UpdateEntry, delEntry: d.DeleteEntry,
		quickSend: d.QuickSend, validate: d.Validate, estimate: d.Estimate, departments: d.Departments, access: d.Access, actions: d.Actions,
		permissions: d.Permissions,
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

func (h *CampaignHandler) seesGroups(r *http.Request, workspaceID string) bool {
	claims := middleware.GetClaims(r)
	if claims == nil || h.permissions == nil {
		return false
	}
	if claims.Role == string(user.RoleAdmin) {
		return true
	}
	return h.permissions.Execute(claims.UserID, workspaceID, workspace_domain.ResourceUnofficialWhatsAppGroups, workspace_domain.ActionRead) == nil
}

// @Summary		Listar campanhas não oficiais
// @Description	Lista as campanhas do WhatsApp não oficial do workspace. `audience` filtra por destino (`leads` ou `groups`); outro valor responde 422. Cada campanha para grupos traz em `targetNames` os nomes dos primeiros grupos; o total está em `metrics.totalNumbers`. `instanceId` filtra pelo número da campanha ou, numa campanha para grupos, pelos números que ela pode usar.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			audience	query		string	false	"Destino"	Enums(leads, groups)
// @Param			instanceId	query		string	false	"Números, separados por vírgula"
// @Param			status		query		string	false	"RUNNING, PAUSED, STOPPED ou COMPLETED"
// @Param			search		query		string	false	"Parte do nome"
// @Param			archived	query		bool	false	"Arquivadas"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]campaignDTO}
// @Failure		403			{object}	response.ErrorResponse
// @Failure		422			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns [get]
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
	audience := uwc.Audience(strings.ToLower(strings.TrimSpace(values.Get("audience"))))
	if audience != "" && !audience.Valid() {
		response.WriteError(w, http.StatusUnprocessableEntity, uwc.ErrCampaignAudienceInvalid.Error(), nil)
		return
	}
	archived := httpx.ParseBoolQuery(values.Get("archived"))
	input := uwc.ListCampaignsInput{
		WorkspaceID:   workspaceID,
		DepartmentIDs: httpx.DepartmentFilterIDs(r),
		InstanceIDs:   httpx.ParseCSVQuery(values["instanceId"]),
		Audience:      audience,
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

// @Summary		Detalhe de uma campanha não oficial
// @Description	Devolve a campanha com as métricas e, enquanto não terminou, `estimate`: quanto falta para enviar os pendentes (`seconds`), pelo intervalo real entre envios de cada número. Numa campanha para grupos os números enviam em paralelo e cada grupo conta no número que pode publicar nele; `unsendable` conta os grupos em que nenhum número pode publicar agora. Sem estimativa disponível o campo não vem.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			id	path		string	true	"Campanha"
// @Success		200	{object}	campaignDTO
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id} [get]
func (h *CampaignHandler) Get(w http.ResponseWriter, r *http.Request) {
	_, _, c, ok := h.ownedCampaign(w, r)
	if !ok {
		return
	}
	if h.estimate != nil && c.Status != campaign.StatusCompleted {
		if remaining, err := h.estimate.Remaining(r.Context(), c); err == nil {
			c.Estimate = remaining
		}
	}
	response.WriteSuccess(w, http.StatusOK, campaignToDTO(c))
}

// @Summary		Estimar o envio de uma campanha não oficial
// @Description	Calcula, antes de criar, quanto tempo a campanha leva para enviar (`estimate.seconds`), pelo mesmo intervalo entre envios que o envio usa. Para pessoas (`audience=leads`) usa o número escolhido e `targets`. Para grupos (`audience=groups`) não há número: para cada grupo de `groupJids` mostra quais dos seus números podem publicar nele agora (`senders`) ou por que nenhum pode (`block`: `not_in_group`, `cannot_post` ou `offline`), e os números enviam em paralelo. Grupos pedem também `unofficial_whatsapp_groups:read`.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			body	body		campaignEstimatePayload	true	"Destino, número ou grupos e ritmo"
// @Success		200		{object}	campaignEstimateDraftDTO
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/estimate [post]
func (h *CampaignHandler) Estimate(w http.ResponseWriter, r *http.Request) {
	workspaceID, scope, ok := h.campaignScope(w, r)
	if !ok {
		return
	}
	var payload campaignEstimatePayload
	if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
		response.WriteInvalidBodyError(w, nil)
		return
	}
	in := payload.toDomain(workspaceID)
	if in.Audience == uwc.AudienceGroups && !h.seesGroups(r, workspaceID) {
		response.WriteError(w, http.StatusForbidden, "you do not have access to this workspace's groups", nil)
		return
	}
	if h.estimate == nil {
		response.WriteError(w, http.StatusServiceUnavailable, uwcuc.ErrGroupTargetsUnavailable.Error(), nil)
		return
	}
	draft, err := h.estimate.Draft(r.Context(), in, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, draftEstimateToDTO(draft))
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

// @Summary		Números e grupos de uma campanha não oficial
// @Description	Lista os destinos da campanha. Depois do envio, `senderInstanceId` e `senderLabel` dizem qual número enviou; numa campanha para grupos cada grupo pode ter saído por um número diferente.
// @Tags			Unofficial WhatsApp Campaigns
// @Produce		json
// @Param			id			path		string	true	"Campanha"
// @Param			status		query		string	false	"Status do envio"
// @Param			search		query		string	false	"Número ou nome"
// @Param			errorCode	query		int		false	"Código de falha"
// @Param			page		query		int		false	"Página"
// @Param			pageSize	query		int		false	"Itens por página"
// @Success		200			{object}	response.PaginatedPayload{data=[]campaignEntryDTO}
// @Failure		403			{object}	response.ErrorResponse
// @Failure		404			{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns/{id}/entries [get]
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

// @Summary		Criar uma campanha não oficial
// @Description	Cria uma campanha parada do WhatsApp não oficial com os números enviados. O contato só usa a saída que vê: cada variação da mensagem precisa mostrar um comando com barra, como `Responda /sair para não receber mais mensagens.`, e sem ele a criação responde 422 `unofficial_campaign_needs_exit`, sem exceção. Um áudio não mostra legenda ao contato: nele o comando vai em `message.exitText`, um texto curto enviado logo depois do áudio na mesma conversa, e um áudio sem esse texto é recusado do mesmo jeito. Se o texto de saída falhar depois do áudio, o contato fica SENT com o código 910005 e o motivo, sem reenviar o áudio. Um link (`https://loja.com/sair`) ou uma data (`12/10`) não contam. Depois do envio, quem responde o comando mostrado na última mensagem de campanha que recebeu nesta conversa deixa de receber mensagens, sem mensagem de confirmação. O fluxo e o agente são conferidos pelo passo compartilhado.
// @Tags			Unofficial WhatsApp Campaigns
// @Accept			json
// @Produce		json
// @Param			body	body		object	true	"Campos da campanha e números"
// @Success		201		{object}	map[string]interface{}
// @Failure		400		{object}	response.ErrorResponse
// @Failure		403		{object}	response.ErrorResponse
// @Failure		404		{object}	response.ErrorResponse
// @Failure		422		{object}	response.ErrorResponse
// @Failure		503		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/unofficial-whatsapp/campaigns [post]
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
	if draft.TargetsGroups() && !h.seesGroups(r, workspaceID) {
		response.WriteError(w, http.StatusForbidden, "you do not have access to this workspace's groups", nil)
		return
	}

	created, err := h.create.Execute(r.Context(), draft, scope)
	if err != nil {
		writeCampaignError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusCreated, campaignToDTO(created))
}

// @Summary		Editar uma campanha não oficial
// @Description	Atualiza mensagem, número, ritmo, fluxo e agente de uma campanha que não está em andamento. Uma campanha preparada a partir de leads (origem `lead_selection`) recusa a troca do modelo, do número ou da mensagem com 409 `send_selection_locked`. O fluxo e o agente são conferidos pelo passo compartilhado: fluxo de outro workspace responde 403 `campaign_workflow_forbidden`, fluxo ou agente inexistente 422, variável do agente ausente 400 `AGENT_REQUIRED_VARIABLE_MISSING`, passo indisponível 503. Uma mensagem nova precisa mostrar ao contato um comando de saída, como `/sair`, em cada variação, ou no `exitText` de um áudio: sem ele responde 422 `unofficial_campaign_needs_exit`. Editar outros campos de uma campanha antiga mantém a mensagem dela sem essa exigência.
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
		errors.Is(err, uwc.ErrCampaignAudienceMismatch),
		errors.Is(err, uwcuc.ErrQuickSendBusy):
		response.WriteError(w, http.StatusConflict, err.Error(), nil)

	case errors.Is(err, uwc.ErrCampaignNeedsExit):
		response.WriteErrorWithCode(w, http.StatusUnprocessableEntity, uwc.ErrorCode(err), err.Error(), nil)

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
		errors.Is(err, uwc.ErrCampaignAudienceInvalid),
		errors.Is(err, uwc.ErrGroupCampaignTakesNoNumber),
		errors.Is(err, uwc.ErrGroupCampaignNoNumber),
		errors.Is(err, uwc.ErrGroupCampaignFunnel),
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
		errors.Is(err, uwc.ErrMessageExitTextTooLong),
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
		errors.Is(err, campaign.ErrLeadTargetsUnavailable),
		errors.Is(err, uwcuc.ErrGroupTargetsUnavailable):
		response.WriteError(w, http.StatusServiceUnavailable, err.Error(), nil)

	default:
		response.WriteError(w, http.StatusInternalServerError, "campaign request failed", nil)
	}
}
