package lead

import (
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/response"
	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"
	"vozko/domain/lead_message_window"
	"vozko/domain/shared"
	"vozko/domain/unofficial_whatsapp"
	businessphone "vozko/domain/whatsapp/business_phone"
	wc_entry "vozko/domain/whatsapp_campaign_entry"
	workspace_domain "vozko/domain/workspace"
	"vozko/infra/http/middleware"
)

type LeadHandler struct {
	leadRepo     leaddomain.Repository
	wcEntryRepo  wc_entry.Repository
	messageRepo  conversation.MessageRepository
	windowRepo   lead_message_window.Repository
	analysisRepo ca.ConversationReader
	phoneRepo    businessphone.Repository
	metaAPI      businessphone.MetaAPIService

	// inboxSeeder queues the unofficial WhatsApp conversations an import can
	// open for the leads it created. Optional, and nil in a deployment without
	// that channel: an import must still work when nothing can seed.
	inboxSeeder InboxSeeder

	// authorizer gates seeding, which is a CHANNEL privilege rather than a lead
	// one. Nil refuses to seed, never allows it.
	authorizer conversation.ConversationAuthorizer
}

// InboxSeeder hands an import's numbers to the background job that opens their
// conversations.
//
// Declared here rather than imported so the lead handler does not depend on a
// channel package. Seeding is the unofficial WhatsApp channel's concern; from
// the import's side it is one collaborator that accepts numbers and answers how
// many it took.
type InboxSeeder interface {
	Publish(in unofficial_whatsapp.SeedRequest) (int, error)
}

// SetInboxSeeder attaches the seeding job. A handler without one simply reports
// nothing seeded.
func (h *LeadHandler) SetInboxSeeder(seeder InboxSeeder) {
	h.inboxSeeder = seeder
}

func NewLeadHandler(
	leadRepo leaddomain.Repository,
	wcEntryRepo wc_entry.Repository,
	messageRepo conversation.MessageRepository,
	windowRepo lead_message_window.Repository,
	analysisRepo ca.ConversationReader,
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

	analyses := []*ca.Analysis{}

	if !entryTypeParam.Valid() || entryTypeParam == shared.EntryTypeWhatsApp {
		wcEntries, err := h.wcEntryRepo.ListByLeadID(leadID)
		if err == nil {
			// One read for the whole campaign rather than one per entry. The
			// engine keys a conversation uniquely, so there is exactly one
			// analysis per entry and no sorting by time to pick a winner.
			entryIDs := make([]string, 0, len(wcEntries))
			for _, entry := range wcEntries {
				if entry.CampaignID == campaignID {
					entryIDs = append(entryIDs, entry.ID)
				}
			}
			found, err := h.analysisRepo.LatestByEntries(r.Context(), workspaceID, ca.SourceWhatsApp, entryIDs)
			if err == nil {
				for _, id := range entryIDs {
					if a, ok := found[id]; ok && a != nil {
						analyses = append(analyses, a)
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

// @Summary		Renomear um lead
// @Description	Define o nome de exibição de um lead. Enviar um nome vazio remove o nome, e o lead volta a ser exibido pelo número.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			id		path		string				true	"Identificador do lead"
// @Param			request	body		RenameLeadRequest	true	"Novo nome"
// @Success		200	{object}	lead.Lead
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id} [patch]
func (h *LeadHandler) RenameLead(w http.ResponseWriter, r *http.Request) {
	leadID := mux.Vars(r)["id"]
	workspaceID := middleware.GetWorkspaceID(r)

	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}
	if leadID == "" {
		response.WriteError(w, http.StatusBadRequest, "Lead ID is required", nil)
		return
	}

	var req RenameLeadRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (empty clears the name)",
		})
		return
	}
	// A missing field is a malformed request. Treating it as an empty string
	// would turn a client bug into a silent data loss: PATCH {} would erase the
	// name of every lead it touched.
	if req.Name == nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"name": "string (required; send \"\" to clear the name)",
		})
		return
	}

	if err := leaddomain.ValidateName(*req.Name); err != nil {
		response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
		return
	}

	// Rename is workspace-scoped in the query itself, so a guessed id from
	// another workspace affects nothing and reports not-found.
	if err := h.leadRepo.Rename(workspaceID, leadID, *req.Name); err != nil {
		if errors.Is(err, leaddomain.ErrLeadNotFound) {
			response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
			return
		}
		if errors.Is(err, leaddomain.ErrLeadNameTooLong) {
			response.WriteError(w, http.StatusBadRequest, err.Error(), nil)
			return
		}
		response.WriteError(w, http.StatusInternalServerError, "Failed to rename lead", nil)
		return
	}

	updated, err := h.leadRepo.FindByID(workspaceID, leadID)
	if err != nil {
		response.WriteError(w, http.StatusNotFound, "Lead not found", nil)
		return
	}
	// The stored value is echoed back, not the submitted one: the name was
	// normalised on the way in, and the UI should show what it will see on the
	// next load rather than what was typed.
	response.WriteSuccess(w, http.StatusOK, updated)
}

// @Summary		Importar leads
// @Description	Cria leads em massa a partir de uma lista de contatos já processada pelo cliente (por exemplo, um CSV lido no navegador). Números são normalizados para o formato brasileiro canônico e deduplicados; linhas inválidas ou repetidas são reportadas, nunca descartadas em silêncio. Leads já existentes no workspace são contabilizados como "matched" e nunca sobrescritos.
// @Tags			Leads
// @Accept			json
// @Produce		json
// @Param			request	body		lead.ImportLeadsRequest	true	"Linhas do arquivo"
// @Success		200		{object}	lead.ImportLeadsResponse
// @Failure		400		{object}	response.ErrorResponse
// @Failure		413		{object}	response.ErrorResponse
// @Failure		500		{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/import [post]
func (h *LeadHandler) ImportLeads(w http.ResponseWriter, r *http.Request) {
	workspaceID := middleware.GetWorkspaceID(r)
	if workspaceID == "" {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
		return
	}

	var req ImportLeadsRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		response.WriteInvalidBodyError(w, map[string]string{
			"rows": "array of {line, number, name?, age?}",
		})
		return
	}

	if len(req.Rows) == 0 {
		response.WriteError(w, http.StatusBadRequest, "Import has no rows", nil)
		return
	}

	// Refused, not truncated. Importing the first 20.000 of 50.000 rows and
	// reporting success is how a campaign goes out to two-fifths of a list
	// while everyone believes it reached all of it.
	if len(req.Rows) > leaddomain.MaxImportRows {
		response.WriteError(w, http.StatusRequestEntityTooLarge, "Too many rows for a single import", map[string]string{
			"maxRows": strconv.Itoa(leaddomain.MaxImportRows),
			"sent":    strconv.Itoa(len(req.Rows)),
		})
		return
	}

	policy, ok := leaddomain.ParseExistingPolicy(req.OnExisting)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Invalid onExisting policy", map[string]string{
			"onExisting": string(leaddomain.PolicyFillEmpty) + " | " + string(leaddomain.PolicySkip),
		})
		return
	}

	rows := make([]leaddomain.ImportRow, 0, len(req.Rows))
	for i, row := range req.Rows {
		line := row.Line
		if line <= 0 {
			// A client that did not send line numbers still gets usable
			// rejections, counted from the order it sent.
			line = i + 1
		}
		rows = append(rows, leaddomain.ImportRow{
			Line:   line,
			Number: row.Number,
			Name:   row.Name,
			Age:    row.Age,
		})
	}

	// The browser vetted these already so the operator could see the outcome
	// before committing. Vetting them again is not redundancy: this endpoint is
	// reachable without that UI, and the rules that decide what a lead IS
	// belong to the domain, not to a form.
	prepared := leaddomain.PrepareImport(rows)

	outcome, err := h.leadRepo.ImportMany(workspaceID, prepared.Inputs, policy)
	if err != nil {
		log.Printf("[leads] import failed for workspace %s: %v", workspaceID, err)
		response.WriteError(w, http.StatusInternalServerError, "An error has occured when trying to import the leads", nil)
		return
	}

	out := ImportLeadsResponse{
		Created:  outcome.Created,
		Matched:  outcome.Matched,
		Blocked:  outcome.Blocked,
		Rejected: []ImportRejection{},
	}
	for _, rejection := range prepared.Rejected {
		switch rejection.Reason {
		case leaddomain.ReasonInvalid:
			out.Invalid++
		case leaddomain.ReasonDuplicate:
			out.Duplicate++
		}
	}
	reported := prepared.Rejected
	if len(reported) > MaxReportedRejections {
		out.RejectedTruncated = len(reported) - MaxReportedRejections
		reported = reported[:MaxReportedRejections]
	}
	for _, rejection := range reported {
		out.Rejected = append(out.Rejected, ImportRejection{
			Line:   rejection.Line,
			Number: rejection.Number,
			Reason: string(rejection.Reason),
		})
	}

	// Seeding runs AFTER the leads exist and never gates the response.
	//
	// The leads are already committed at this point, so a seeding failure is
	// reported alongside a successful import rather than as a failed one: an
	// operator told the import failed runs it again, and the second run finds
	// every number matched and creates nothing, which looks like the CRM lost
	// their file.
	if req.SeedInbox {
		// A channel privilege, not a lead one. The route proved leads:create;
		// opening conversations with numbers that never wrote in is
		// unofficial_whatsapp_instances:send. Reported beside a successful
		// import rather than failing it: the leads are already committed, and
		// telling the operator the import failed would have them run it again.
		claims := middleware.GetClaims(r)
		if claims == nil || !h.maySeedInbox(claims.UserID, workspaceID, claims.Role) {
			out.InboxSeedError = "You don't have permission to start conversations on the unofficial WhatsApp channel"
		} else {
			out.InboxSeedQueued, out.InboxSeedError = h.queueInboxSeed(workspaceID, prepared.Inputs)
		}
	}

	response.WriteSuccess(w, http.StatusOK, out)
}

// queueInboxSeed hands the imported numbers to the seeding job.
//
// Every input goes, matched leads included, not only the newly created ones.
// A number the workspace already knew is exactly the case where a lead exists
// on the leads page with no way to reach it from the inbox, and seeding is
// idempotent: a conversation that already carries messages is left alone.
func (h *LeadHandler) queueInboxSeed(workspaceID string, inputs []leaddomain.BulkLeadInput) (int, string) {
	if h.inboxSeeder == nil {
		return 0, "Inbox seeding is not available on this deployment"
	}

	targets := make([]unofficial_whatsapp.SeedTarget, 0, len(inputs))
	for _, input := range inputs {
		targets = append(targets, unofficial_whatsapp.SeedTarget{
			Number: input.Number,
			Name:   input.Name,
		})
	}

	queued, err := h.inboxSeeder.Publish(unofficial_whatsapp.SeedRequest{
		WorkspaceID: workspaceID,
		Targets:     targets,
	})
	if err != nil {
		log.Printf("[leads] inbox seeding failed to queue for workspace %s: %v", workspaceID, err)
		return queued, "The leads were imported, but their conversations could not be queued"
	}
	return queued, ""
}

// SetAuthorizer attaches the gate seeding needs.
//
// The import route proves leads:create. Seeding is a different act on a
// different resource: it OPENS conversations with numbers that never contacted
// us, on a connected unofficial WhatsApp number, in bulk. That is precisely
// what unofficial_whatsapp_instances:send exists to withhold — the catalogue
// calls it out as separate from update so an attendant who may answer cannot
// start conversations with arbitrary numbers.
//
// It reuses conversation.ConversationAuthorizer rather than declaring another
// one-method port: the interface already exists in the domain, this package
// already imports it, and a second declaration would be the same contract
// written twice.
func (h *LeadHandler) SetAuthorizer(a conversation.ConversationAuthorizer) {
	h.authorizer = a
}

// maySeedInbox reports whether this caller may open conversations on the
// unofficial WhatsApp channel. A nil authorizer refuses: an unwired gate must
// fail closed, or a wiring mistake silently hands out the privilege.
func (h *LeadHandler) maySeedInbox(userID, workspaceID, role string) bool {
	if h.authorizer == nil {
		return false
	}
	return h.authorizer.HasWorkspacePermission(
		userID, workspaceID,
		string(workspace_domain.ResourceUnofficialWhatsAppInstances),
		string(workspace_domain.ActionSend),
		role == "admin",
	)
}
