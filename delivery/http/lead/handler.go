package lead

import (
	"context"
	"errors"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/mux"

	"vozko/delivery/http/httpx"
	"vozko/delivery/http/response"
	ca "vozko/domain/audience"
	"vozko/domain/conversation"
	leaddomain "vozko/domain/lead"

	"vozko/domain/shared"
	wc_entry "vozko/domain/whatsapp_campaign_entry"
	lead_usecase "vozko/usecases/lead"
)

type Commands interface {
	Create(ctx context.Context, a lead_usecase.Actor, d leaddomain.Draft) (lead_usecase.CreateResult, error)
	Update(ctx context.Context, a lead_usecase.Actor, id string, expected *int64, e leaddomain.Edit) (*leaddomain.Lead, error)
	Rename(ctx context.Context, a lead_usecase.Actor, id string, expected *int64, name string) (*leaddomain.Lead, error)
	Block(ctx context.Context, a lead_usecase.Actor, id string, in lead_usecase.BlockInput) (lead_usecase.BlockResult, error)
	SetOwner(ctx context.Context, a lead_usecase.Actor, id, owner string) (*leaddomain.Lead, error)
	OptOut(ctx context.Context, a lead_usecase.Actor, id string, source leaddomain.OptOutSource) (*leaddomain.Lead, error)
	AddRelative(ctx context.Context, a lead_usecase.Actor, id string, in lead_usecase.AddRelativeInput) (lead_usecase.RelativeResult, error)
	LinkRelation(ctx context.Context, a lead_usecase.Actor, id, otherID string, kind leaddomain.RelationKind) (lead_usecase.RelationResult, error)
	RemoveRelation(ctx context.Context, a lead_usecase.Actor, relationID string) (leaddomain.Relation, error)
	SetArea(ctx context.Context, a lead_usecase.Actor, id string, area leaddomain.Area) (*leaddomain.Lead, error)
	Anonymize(ctx context.Context, a lead_usecase.Actor, id string) (leaddomain.Erasure, error)
}

type History interface {
	Detail(ctx context.Context, v conversation.Viewer, leadID string) (lead_usecase.LeadDetail, error)
	EntriesInCampaign(v conversation.Viewer, leadID, campaignID string, entryType shared.EntryType) ([]wc_entry.WhatsAppCampaignEntry, error)
	Analyses(ctx context.Context, v conversation.Viewer, leadID, campaignID string, entryType shared.EntryType) (*leaddomain.Lead, []*ca.Analysis, error)
	EntryConversation(v conversation.Viewer, entryID string, entryType shared.EntryType) (lead_usecase.EntryConversation, error)
	Relatives(ctx context.Context, v conversation.Viewer, q leaddomain.RelativesQuery) (leaddomain.RelativesPage, error)
	EntryLead(ctx context.Context, v conversation.Viewer, entryID string, entryType shared.EntryType) (leaddomain.Card, error)
}

type Summaries interface {
	Summary(ctx context.Context, v conversation.Viewer, leadID string) (lead_usecase.LeadDetailSummary, error)
}

type Pages interface {
	List(ctx context.Context, a lead_usecase.Actor, in leaddomain.ListLeadsInput) (*shared.PaginatedResult[*leaddomain.LeadWithSummary], error)
}

type HandlerDeps struct {
	Leads     leaddomain.Queries
	Repo      leaddomain.Repository
	Commands  Commands
	History   History
	Summaries Summaries
	Pages     Pages
	Sections  Sections
	Imports   Imports
	Timeline  Timeline
	Actions   Actions
	Locations Locations
	Sends     Sends
	Now       func() time.Time
}

type LeadHandler struct {
	leads     leaddomain.Queries
	leadRepo  leaddomain.Repository
	commands  Commands
	history   History
	summaries Summaries
	pages     Pages
	sections  Sections
	imports   Imports
	timeline  Timeline
	actions   Actions
	locations Locations
	sends     Sends
	now       func() time.Time
}

func NewLeadHandler(deps HandlerDeps) *LeadHandler {
	now := deps.Now
	if now == nil {
		now = time.Now
	}
	return &LeadHandler{
		leads:     deps.Leads,
		leadRepo:  deps.Repo,
		commands:  deps.Commands,
		history:   deps.History,
		summaries: deps.Summaries,
		pages:     deps.Pages,
		sections:  deps.Sections,
		imports:   deps.Imports,
		timeline:  deps.Timeline,
		actions:   deps.Actions,
		locations: deps.Locations,
		sends:     deps.Sends,
		now:       now,
	}
}

var actorOf = httpx.LeadActor

func (h *LeadHandler) requestActor(w http.ResponseWriter, r *http.Request) (lead_usecase.Actor, bool) {
	a, ok := actorOf(r)
	if !ok {
		response.WriteError(w, http.StatusBadRequest, "Workspace context is required", nil)
	}
	return a, ok
}

func (h *LeadHandler) historyReady(w http.ResponseWriter) bool {
	if h.history == nil {
		writeHistoryUnavailable(w)
		return false
	}
	return true
}

func writeHistoryUnavailable(w http.ResponseWriter) {
	response.WriteErrorWithCode(w, http.StatusServiceUnavailable, "lead_history_unavailable", "O histórico do lead não está disponível neste servidor", nil)
}

func writeLeadReadError(w http.ResponseWriter, err error) {
	if errors.Is(err, leaddomain.ErrRelativesQueryInvalid) || errors.Is(err, leaddomain.ErrPageQueryInvalid) {
		response.WriteErrorWithCode(w, http.StatusBadRequest, leaddomain.ErrorCode(err), err.Error(), nil)
		return
	}
	if errors.Is(err, leaddomain.ErrLeadNotFound) || errors.Is(err, leaddomain.ErrLeadRequired) {
		response.WriteErrorWithCode(w, http.StatusNotFound, leaddomain.ErrorCode(leaddomain.ErrLeadNotFound), "Lead not found", nil)
		return
	}
	if errors.Is(err, leaddomain.ErrLeadForbidden) {
		response.WriteErrorWithCode(w, http.StatusForbidden, leaddomain.ErrorCode(err), err.Error(), nil)
		return
	}
	response.WriteError(w, http.StatusInternalServerError, "Failed to fetch lead", nil)
}

// @Summary		Obter lead por ID
// @Description	Retorna o registro completo do lead (identidade, apelido, e-mail, nascimento, responsável, consentimento, contagens e `version`) com o histórico de campanhas e o status da janela do WhatsApp. A lista de campanhas traz só as conversas que você pode abrir: conversas de outros departamentos, ou de outras pessoas quando você não pode ver as conversas dos colegas, não aparecem. Traz também os telefones de contato (`phones`), os endereços (`addresses`, com `geoStatus` e, quando já localizado, a posição e a precisão). A família vem só contada (`relativesCount`, parentes, e `referredCount`, pessoas que este lead indicou); a lista sai página por página em GET /leads/{id}/relatives, quando a aba Família é aberta. Os campos personalizados vêm em `customFields`: os marcados como sensíveis só aparecem para quem tem `leads:read_sensitive`, e valores de campos que não existem mais nunca saem do servidor. Sem `leads:read_addresses`, cada endereço traz só bairro, cidade, UF e o código IBGE da cidade (sem CEP, rua, número, complemento nem posição). O consentimento (`whatsappOptIn`) traz a data, a origem e a finalidade (`purpose`) registrada. `ownerName` é o nome do responsável (ausente quando o lead não tem responsável; se o nome não puder ser lido, a resposta é 500, nunca uma ficha sem o nome). As contagens das abas Negócios e Memórias e os outros leads que têm os mesmos números saem em GET /leads/{id}/summary, para que uma falha nelas não derrube a ficha. Envie a `version` recebida no cabeçalho `If-Match` ao editar.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead (UUID)"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id} [get]
func (h *LeadHandler) GetByID(w http.ResponseWriter, r *http.Request) {
	h.writeDetail(w, r, mux.Vars(r)["id"])
}

// @Summary		Histórico de campanhas do lead
// @Description	Mesmo conteúdo de GET /leads/{id}: o lead com as campanhas e só as conversas que você pode abrir.
// @Tags			Leads
// @Produce		json
// @Param			id	path		string	true	"ID do lead (UUID)"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/{id}/campaigns [get]
func (h *LeadHandler) GetCampaignHistory(w http.ResponseWriter, r *http.Request) {
	h.writeDetail(w, r, mux.Vars(r)["id"])
}

// @Summary		Buscar lead por número
// @Description	Retorna o lead do workspace com o número informado, no mesmo formato de GET /leads/{id}.
// @Tags			Leads
// @Produce		json
// @Param			number	query		string	true	"Número de telefone do lead"
// @Success		200	{object}	lead.LeadDetailResponse
// @Failure		400	{object}	response.ErrorResponse
// @Failure		403	{object}	response.ErrorResponse
// @Failure		404	{object}	response.ErrorResponse
// @Failure		500	{object}	response.ErrorResponse
// @Failure		503	{object}	response.ErrorResponse
// @Security		BearerAuth
// @Router			/leads/search [get]
func (h *LeadHandler) GetByNumber(w http.ResponseWriter, r *http.Request) {
	a, ok := h.requestActor(w, r)
	if !ok {
		return
	}
	number := strings.TrimSpace(r.URL.Query().Get("number"))
	if number == "" {
		response.WriteError(w, http.StatusBadRequest, "Phone number is required", nil)
		return
	}
	found, err := h.leads.GetByNumber(a.WorkspaceID, number)
	if err != nil {
		if errors.Is(err, leaddomain.ErrLeadInvalid) {
			response.WriteError(w, http.StatusBadRequest, "Invalid phone number format", nil)
			return
		}
		writeLeadReadError(w, err)
		return
	}
	h.writeDetail(w, r, found.ID)
}

func (h *LeadHandler) writeDetail(w http.ResponseWriter, r *http.Request, leadID string) {
	a, ok := h.requestActor(w, r)
	if !ok || !h.historyReady(w) {
		return
	}
	detail, err := h.history.Detail(r.Context(), a, leadID)
	if err != nil {
		writeLeadReadError(w, err)
		return
	}
	response.WriteSuccess(w, http.StatusOK, toLeadDetail(detail, h.now()))
}

func leadDetailSummaryResponse(s lead_usecase.LeadDetailSummary) LeadDetailSummaryResponse {
	return LeadDetailSummaryResponse{DealsCount: s.DealsCount, MemoriesCount: s.MemoriesCount, SharedNumbers: sharedNumberResponses(s.SharedNumbers)}
}

func toLeadDetail(d lead_usecase.LeadDetail, now time.Time) LeadDetailResponse {
	out := LeadDetailResponse{
		LeadRecordResponse: toLeadRecord(d.Lead, now),
		OwnerName:          d.OwnerName,
		WhatsAppCampaigns:  d.Summary.WhatsAppCampaigns,
		TotalCampaigns:     d.Summary.TotalCampaigns,
		LastActivityAt:     fmtTimePtr(d.Summary.LastActivityAt),
		WhatsAppWindowOpen: d.Summary.WhatsAppWindowOpen,
		WindowExpiresAt:    fmtTimePtr(d.Summary.WindowExpiresAt),
		Campaigns:          make([]CampaignHistoryItem, 0, len(d.Campaigns)),
	}
	for _, c := range d.Campaigns {
		entries := make([]CampaignEntryItem, 0, len(c.Entries))
		for _, e := range c.Entries {
			entries = append(entries, CampaignEntryItem{
				ID:        e.ID,
				Status:    string(e.Status),
				CreatedAt: fmtRFC3339(e.CreatedAt),
				UpdatedAt: fmtRFC3339(e.UpdatedAt),
			})
		}
		out.Campaigns = append(out.Campaigns, CampaignHistoryItem{
			CampaignID:   c.CampaignID,
			CampaignName: c.CampaignName,
			Type:         string(shared.EntryTypeWhatsApp),
			Entries:      entries,
		})
	}
	return out
}
