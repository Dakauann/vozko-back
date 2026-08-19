package lead

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	"vozko/domain/analysis"
	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"
	"vozko/domain/lead_message_window"
	"vozko/domain/shared"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc_entry "vozko/domain/whatsapp_campaign_entry"
	"vozko/infra/http/middleware"
)

type LeadHandler struct {
	leadRepo     leaddomain.Repository
	wcEntryRepo  wc_entry.Repository
	messageRepo  conversation.MessageRepository
	windowRepo   lead_message_window.Repository
	analysisRepo analysis.Repository
	phoneRepo    businessphone.Repository
	metaAPI      businessphone.MetaAPIService
}

func NewLeadHandler(
	leadRepo leaddomain.Repository,
	wcEntryRepo wc_entry.Repository,
	messageRepo conversation.MessageRepository,
	windowRepo lead_message_window.Repository,
	analysisRepo analysis.Repository,
	phoneRepo businessphone.Repository,
	metaAPI businessphone.MetaAPIService,
) *LeadHandler {
	return &LeadHandler{
		leadRepo:     leadRepo,
		wcEntryRepo:  wcEntryRepo,
		messageRepo:  messageRepo,
		windowRepo:   windowRepo,
		analysisRepo: analysisRepo,
		phoneRepo:    phoneRepo,
		metaAPI:      metaAPI,
	}
}

type entryAccumulator struct {
	campaignID   string
	campaignName string
	entryType    string
	entries      []CampaignEntryItem
	latest       time.Time
}

func fmtRFC3339(t time.Time) string {
	return t.UTC().Format(time.RFC3339)
}

func fmtTimePtr(t *time.Time) *string {
	if t == nil {
		return nil
	}
	s := fmtRFC3339(*t)
	return &s
}

// @Summary		Obter lead por ID
// @Description	Retorna os detalhes de um lead do workspace, incluindo o histórico de campanhas e o status da janela de atendimento do WhatsApp.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id} [get]
func (h *LeadHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	leadID := mux.Vars(r)["id"]
	if leadID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
		return
	}
	if leadRecord == nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	resp := h.buildLeadDetailResponse(workspaceID, leadRecord)
	response.WriteSuccess(w, http.StatusOK, resp)
}

// @Summary		Buscar lead por número
// @Description	Retorna os detalhes de um lead do workspace a partir do número de telefone informado.
// @Tags			Leads
// @Produce		json
// @Param			number	query		string	true	"Número de telefone do lead"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/search [get]
func (h *LeadHandler) GetByNumber(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	number := strings.TrimSpace(r.URL.Query().Get("number"))
	if number == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone number is required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByNumber(workspaceID, number)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		if errors.Is(err, leaddomain.ErrLeadInvalid) {
			response.WriteError(w, http.StatusBadRequest, "Invalid phone number format", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
		return
	}
	if leadRecord == nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	resp := h.buildLeadDetailResponse(workspaceID, leadRecord)
	response.WriteSuccess(w, http.StatusOK, resp)
}

// @Summary		Listar leads
// @Description	Retorna a lista paginada de leads do workspace. Aceita os filtros simples por querystring (nome, número, período, faixa etária, campanha, canal, bloqueio, janela, memórias) e/ou um filtro estruturado `filter` (crmfilter em JSON, opcionalmente em base64) com grupos AND/OR. A ordenação aceita múltiplas chaves.
// @Tags			Leads
// @Produce		json
// @Param			page				query	int		false	"Número da página (inicia em 1)"
// @Param			pageSize			query	int		false	"Itens por página (máximo 200)"
// @Param			sort				query	string	false	"Ordenação: createdAt, updatedAt, lastActivityAt, name, number, age, campaigns, memories, lastMemoryAt (ex.: lastActivityAt:desc,name:asc)"
// @Param			order				query	string	false	"Direção padrão quando o sort não a informa ('asc' ou 'desc')"
// @Param			filter				query	string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q					query	string	false	"Busca livre por nome, número ou conteúdo das memórias"
// @Param			number				query	string	false	"Filtrar por número de telefone"
// @Param			name				query	string	false	"Filtrar por nome"
// @Param			hasName				query	bool	false	"Possui nome preenchido"
// @Param			ageFrom				query	int		false	"Idade mínima"
// @Param			ageTo				query	int		false	"Idade máxima"
// @Param			blocked				query	bool	false	"Somente bloqueados / não bloqueados"
// @Param			windowOpen			query	bool	false	"Janela de 24h aberta"
// @Param			channel				query	[]string	false	"Canais (whatsapp, unofficial_whatsapp, telegram, instagram)"
// @Param			hasWhatsAppCampaign	query	bool	false	"Possui campanha de WhatsApp"
// @Param			campaignId			query	[]string	false	"IDs de campanha"
// @Param			campaignStatus		query	[]string	false	"Status de envio na campanha"
// @Param			campaignsFrom		query	int		false	"Mínimo de campanhas"
// @Param			campaignsTo			query	int		false	"Máximo de campanhas"
// @Param			stageId				query	[]string	false	"IDs de etapa do CRM"
// @Param			labelId				query	[]string	false	"IDs de etiqueta do CRM"
// @Param			hasMemory			query	bool	false	"Possui memórias registradas"
// @Param			memoryCategory		query	[]string	false	"Categorias de memória"
// @Param			memoryAuthor		query	[]string	false	"Autor da memória (human, ai, system)"
// @Param			memoryText			query	string	false	"Busca no conteúdo das memórias"
// @Param			memoriesFrom		query	int		false	"Mínimo de memórias"
// @Param			memoriesTo			query	int		false	"Máximo de memórias"
// @Param			memoryFrom			query	string	false	"Memória atualizada a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			memoryTo			query	string	false	"Memória atualizada até (RFC3339 ou YYYY-MM-DD)"
// @Param			createdFrom			query	string	false	"Criados a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			createdTo			query	string	false	"Criados até (RFC3339 ou YYYY-MM-DD)"
// @Param			updatedFrom			query	string	false	"Atualizados a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			updatedTo			query	string	false	"Atualizados até (RFC3339 ou YYYY-MM-DD)"
// @Param			activityFrom		query	string	false	"Última atividade a partir de (RFC3339 ou YYYY-MM-DD)"
// @Param			activityTo			query	string	false	"Última atividade até (RFC3339 ou YYYY-MM-DD)"
// @Success		200	{array}		lead.LeadListResponseItem
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads [get]
func (h *LeadHandler) List(w http.ResponseWriter, r *http.Request) {
	input, ok := h.listInput(w, r)
	if !ok {
		return
	}

	result, err := h.leadRepo.ListWithSummary(input)
	if err != nil {
		h.writeListError(w, err)
		return
	}

	items := make([]LeadListResponseItem, 0, len(result.Items))
	for _, lws := range result.Items {
		items = append(items, toLeadListItem(lws))
	}

	response.WritePaginated(w, http.StatusOK, items, response.PaginationMeta{
		Page:       result.Page,
		PageSize:   result.PageSize,
		TotalPages: result.TotalPages,
		TotalItems: result.TotalItems,
	})
}

// @Summary		Contagens dos filtros de leads
// @Description	Retorna as contagens agregadas do MESMO conjunto filtrado que a listagem devolve, por bloqueio, janela, campanha, memória, canal, categoria de memória e status de envio. Aceita exatamente os mesmos parâmetros de filtro de GET /leads.
// @Tags			Leads
// @Produce		json
// @Param			filter	query		string	false	"Filtro crmfilter em JSON (opcionalmente codificado em base64)"
// @Param			q		query		string	false	"Busca livre por nome, número ou conteúdo das memórias"
// @Success		200	{object}	lead.LeadFacets
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/facets [get]
func (h *LeadHandler) Facets(w http.ResponseWriter, r *http.Request) {
	input, ok := h.listInput(w, r)
	if !ok {
		return
	}

	facets, err := h.leadRepo.Facets(input)
	if err != nil {
		h.writeListError(w, err)
		return
	}

	response.WriteSuccess(w, http.StatusOK, facets)
}

// listInput parses the shared read query for the list and its facet counts.
//
// Both endpoints must read the same query string the same way — a facet badge
// counted over a different set than the rows below it is worse than no badge —
// so there is one parser and both call it.
func (h *LeadHandler) listInput(w http.ResponseWriter, r *http.Request) (leaddomain.ListLeadsInput, bool) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return leaddomain.ListLeadsInput{}, false
	}

	input, err := listInputFromQuery(workspaceID, r.URL.Query())
	if err != nil {
		response.WriteError(w, http.StatusBadRequest, "Invalid filter parameter", nil)
		return leaddomain.ListLeadsInput{}, false
	}
	return input, true
}

// writeListError separates "your query was wrong" from "we failed". A filter
// naming a field the lead object cannot answer is a 400 the client can fix and
// a message they can act on; anything else is ours.
func (h *LeadHandler) writeListError(w http.ResponseWriter, err error) {
	if errors.Is(err, leaddomain.ErrLeadFilterInvalid) {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to fetch leads", nil)
}

// @Summary		Histórico de campanhas do lead
// @Description	Retorna o histórico de campanhas de WhatsApp do lead junto com os detalhes do lead.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns [get]
func (h *LeadHandler) GetCampaignHistory(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	leadID := mux.Vars(r)["id"]
	if leadID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
		return
	}
	if leadRecord == nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	resp := h.buildLeadDetailResponse(workspaceID, leadRecord)
	response.WriteSuccess(w, http.StatusOK, resp)
}

func (h *LeadHandler) resolveCampaignNames(accMap map[string]*entryAccumulator) {
	if len(accMap) == 0 {
		return
	}

	var wcIDs []string
	for _, acc := range accMap {
		if acc.entryType == "whatsapp" {
			wcIDs = append(wcIDs, acc.campaignID)
		}
	}

	names := h.leadRepo.ResolveCampaignNames(wcIDs)
	for _, acc := range accMap {
		key := acc.entryType + ":" + acc.campaignID
		if name, ok := names[key]; ok {
			acc.campaignName = name
		}
	}
}

func (h *LeadHandler) buildLeadDetailResponse(workspaceID string, l *leaddomain.Lead) LeadDetailResponse {
	resp := LeadDetailResponse{
		ID:          l.ID,
		WorkspaceID: l.WorkspaceID,
		Number:      l.Number,
		Name:        l.Name,
		Age:         l.Age,
		Blocked:     l.Blocked,
		BlockedBy:   l.BlockedBy,
		CreatedAt:   fmtRFC3339(l.CreatedAt),
		UpdatedAt:   fmtRFC3339(l.UpdatedAt),
		Campaigns:   make([]CampaignHistoryItem, 0),
	}
	if l.Blocked && !l.BlockedAt.IsZero() {
		blockedAt := fmtRFC3339(l.BlockedAt)
		resp.BlockedAt = &blockedAt
	}

	accMap := make(map[string]*entryAccumulator)

	if wcEntries, err := h.wcEntryRepo.ListByLeadID(l.ID); err == nil {
		for _, e := range wcEntries {
			key := "whatsapp:" + e.CampaignID
			acc, ok := accMap[key]
			if !ok {
				acc = &entryAccumulator{
					campaignID: e.CampaignID,
					entryType:  "whatsapp",
					entries:    make([]CampaignEntryItem, 0),
				}
				accMap[key] = acc
			}
			acc.entries = append(acc.entries, CampaignEntryItem{
				ID:        e.ID,
				Status:    string(e.Status),
				CreatedAt: fmtRFC3339(e.CreatedAt),
				UpdatedAt: fmtRFC3339(e.UpdatedAt),
			})
			if e.UpdatedAt.After(acc.latest) {
				acc.latest = e.UpdatedAt
			}
		}
	}

	h.resolveCampaignNames(accMap)

	summary := &leaddomain.LeadSummary{}

	if windows, err := h.windowRepo.FindAllByLead(l.ID); err == nil {
		for _, w := range windows {
			if w.IsWindowOpen() {
				summary.WhatsAppWindowOpen = true
				exp := w.WindowExpiresAt()
				summary.WindowExpiresAt = &exp
			}
			if summary.LastActivityAt == nil || w.LastMessageAt.After(*summary.LastActivityAt) {
				summary.LastActivityAt = &w.LastMessageAt
			}
		}
	}

	for _, acc := range accMap {
		if acc.entryType == "whatsapp" {
			summary.WhatsAppCampaigns += len(acc.entries)
		}
		if summary.LastActivityAt == nil || acc.latest.After(*summary.LastActivityAt) {
			summary.LastActivityAt = &acc.latest
		}
	}
	summary.TotalCampaigns = summary.WhatsAppCampaigns

	resp.WhatsAppCampaigns = summary.WhatsAppCampaigns
	resp.TotalCampaigns = summary.TotalCampaigns
	resp.LastActivityAt = fmtTimePtr(summary.LastActivityAt)
	resp.WhatsAppWindowOpen = summary.WhatsAppWindowOpen
	resp.WindowExpiresAt = fmtTimePtr(summary.WindowExpiresAt)

	for _, acc := range accMap {
		resp.Campaigns = append(resp.Campaigns, CampaignHistoryItem{
			CampaignID:   acc.campaignID,
			CampaignName: acc.campaignName,
			Type:         acc.entryType,
			Entries:      acc.entries,
		})
	}

	return resp
}

// @Summary		Histórico de conversas do lead
// @Description	Retorna as mensagens trocadas com o lead em todos os canais, com filtro opcional por tipo de entrada.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead"
// @Param			entryType	query		string	false	"Filtrar por tipo de entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/conversations [get]
func (h *LeadHandler) GetConversationHistory(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	leadID := mux.Vars(r)["id"]
	if leadID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
		return
	}
	if leadRecord == nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	entryTypeFilter := shared.EntryType(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entryType"))))

	messages, err := h.messageRepo.ListByLeadID(leadID)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch conversation history", nil)
		return
	}

	if entryTypeFilter.Valid() {
		filtered := make([]*conversation.Message, 0)
		for _, msg := range messages {
			if msg.EntryType == entryTypeFilter {
				filtered = append(filtered, msg)
			}
		}
		messages = filtered
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":   leadID,
		"number":   leadRecord.Number,
		"name":     leadRecord.Name,
		"messages": messages,
	})
}

// @Summary		Bloquear ou desbloquear lead
// @Description	Bloqueia ou desbloqueia um lead no workspace. Quando informado o telefone comercial, o contato também é bloqueado/desbloqueado no lado da Meta (melhor esforço).
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"ID do lead"
// @Param			request	body		BlockLeadRequest	true	"Estado de bloqueio do lead"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/block [post]
func (h *LeadHandler) BlockLead(w http.ResponseWriter, r *http.Request) {
	leadId := mux.Vars(r)["id"]
	workspaceID := middleware.GetWorkspaceID(r)

	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}

	if leadId == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByID(workspaceID, leadId)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	var req BlockLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"blocked": "boolean",
		})
		return
	}

	update := leaddomain.LeadUpdate{Blocked: &req.Blocked}
	if req.Blocked {
		if claims := middleware.GetClaims(r); claims != nil && strings.TrimSpace(claims.UserID) != "" {
			actorID := strings.TrimSpace(claims.UserID)
			update.BlockedBy = &actorID
		}
	}

	if err := h.leadRepo.Update(workspaceID, leadRecord.ID, update); err != nil {
		response.WriteError(w, http.StatusInternalServerError, "An error has occured when trying to update the lead", nil)
		return
	}

	metaApplied := h.applyWhatsAppBlock(workspaceID, req.BusinessPhoneID, leadRecord.Number, req.Blocked)

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":      leadRecord.ID,
		"blocked":     req.Blocked,
		"metaApplied": metaApplied,
	})
}

func (h *LeadHandler) applyWhatsAppBlock(workspaceID, businessPhoneID, contactNumber string, block bool) bool {
	businessPhoneID = strings.TrimSpace(businessPhoneID)
	contactNumber = strings.TrimSpace(contactNumber)
	if businessPhoneID == "" || contactNumber == "" || h.phoneRepo == nil || h.metaAPI == nil {
		return false
	}

	phone, err := h.phoneRepo.FindByID(businessPhoneID)
	if err != nil || phone == nil {
		log.Printf("[lead-block] could not resolve business phone %s: %v", businessPhoneID, err)
		return false
	}
	if !phone.BelongsToWorkspace(workspaceID) {
		log.Printf("[lead-block] business phone %s does not belong to workspace %s", businessPhoneID, workspaceID)
		return false
	}
	if strings.TrimSpace(phone.AccessToken) == "" || strings.TrimSpace(phone.MetaPhoneNumberID) == "" {
		log.Printf("[lead-block] business phone %s missing access token or meta phone id, skipping Meta block", businessPhoneID)
		return false
	}

	if block {
		err = h.metaAPI.BlockUser(phone.MetaPhoneNumberID, contactNumber, phone.AccessToken)
	} else {
		err = h.metaAPI.UnblockUser(phone.MetaPhoneNumberID, contactNumber, phone.AccessToken)
	}
	if err != nil {
		log.Printf("[lead-block] Meta block (block=%v) failed for %s on phone %s: %v", block, contactNumber, businessPhoneID, err)
		return false
	}
	return true
}

// @Summary		Conversa por entrada
// @Description	Retorna as mensagens de uma entrada específica (chamada ou conversa de campanha), identificada pelo tipo e ID da entrada.
// @Tags			Leads
// @Produce		json
// @Param			entryId		path		string	true	"ID da entrada"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/entries/{entryId}/conversation [get]
func (h *LeadHandler) GetConversationByEntry(w http.ResponseWriter, r *http.Request) {
	entryID := mux.Vars(r)["entryId"]
	entryTypeStr := strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entryType")))

	if entryID == "" {
		response.WriteError(w, http.StatusBadRequest, "Entry ID is required", nil)
		return
	}

	entryType := shared.EntryType(entryTypeStr)
	if !entryType.Valid() {
		entryType = shared.EntryTypeWhatsApp
	}

	messages, err := h.messageRepo.ListByEntry(entryID, entryType)
	if err != nil {
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch conversation", nil)
		return
	}

	var leadID, campaignID, status string
	if entryType == shared.EntryTypeWhatsApp {
		entry, err := h.wcEntryRepo.FindByID(entryID)
		if err == nil && entry != nil {
			leadID = entry.LeadID
			campaignID = entry.CampaignID
			status = string(entry.Status)
		}
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"entryId":      entryID,
		"entryType":    entryType,
		"leadId":       leadID,
		"campaignId":   campaignID,
		"status":       status,
		"messages":     messages,
		"messageCount": len(messages),
	})
}

// @Summary		Entradas do lead por campanha
// @Description	Retorna as entradas do lead em uma campanha específica, com filtro opcional por tipo de entrada.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead"
// @Param			campaignId	path		string	true	"ID da campanha"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns/{campaignId}/entries [get]
func (h *LeadHandler) GetEntriesByCampaign(w http.ResponseWriter, r *http.Request) {
	leadID := mux.Vars(r)["id"]
	campaignID := mux.Vars(r)["campaignId"]

	if leadID == "" || campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID and Campaign ID are required", nil)
		return
	}

	entryType := shared.EntryType(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entryType"))))

	var entries []EntryResponse

	if !entryType.Valid() || entryType == shared.EntryTypeWhatsApp {
		wcEntries, err := h.wcEntryRepo.ListByLeadID(leadID)
		if err == nil {
			for _, e := range wcEntries {
				if e.CampaignID == campaignID {
					entries = append(entries, EntryResponse{
						ID:         e.ID,
						CampaignID: e.CampaignID,
						EntryType:  shared.EntryTypeWhatsApp,
						Status:     string(e.Status),
						CreatedAt:  e.CreatedAt.Format("2006-01-02T15:04:05Z"),
					})
				}
			}
		}
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":     leadID,
		"campaignId": campaignID,
		"entries":    entries,
	})
}

// @Summary		Análises do lead por campanha
// @Description	Retorna as análises de IA das entradas do lead em uma campanha específica, com filtro opcional por tipo de entrada.
// @Tags			Leads
// @Produce		json
// @Param			id			path		string	true	"ID do lead"
// @Param			campaignId	path		string	true	"ID da campanha"
// @Param			entryType	query		string	false	"Tipo da entrada ('whatsapp')"
// @Success		200	{object}	map[string]interface{}
// @Failure		400	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns/{campaignId}/analysis [get]
func (h *LeadHandler) GetAnalysisByCampaign(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	leadID := mux.Vars(r)["id"]
	campaignID := mux.Vars(r)["campaignId"]

	if leadID == "" || campaignID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID and Campaign ID are required", nil)
		return
	}

	leadRecord, err := h.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
		return
	}
	if leadRecord == nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}

	entryTypeParam := shared.EntryType(strings.ToLower(strings.TrimSpace(r.URL.Query().Get("entryType"))))

	var analyses []*analysis.Analysis

	if !entryTypeParam.Valid() || entryTypeParam == shared.EntryTypeWhatsApp {
		wcEntries, err := h.wcEntryRepo.ListByLeadID(leadID)
		if err == nil {
			for _, entry := range wcEntries {
				if entry.CampaignID == campaignID {
					entryAnalyses, err := h.analysisRepo.ListByEntry(entry.ID, shared.EntryTypeWhatsApp)
					if err == nil {
						for i := range entryAnalyses {
							analyses = append(analyses, &entryAnalyses[i])
						}
					}
				}
			}
		}
	}

	response.WriteSuccess(w, http.StatusOK, map[string]interface{}{
		"leadId":     leadID,
		"campaignId": campaignID,
		"number":     leadRecord.Number,
		"name":       leadRecord.Name,
		"analyses":   analyses,
	})
}
